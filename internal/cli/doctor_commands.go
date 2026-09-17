package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/doctor"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/instructions"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

func instructionsAction(s *boundary, topic string) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		if cmd.Args().Len() > 0 {
			return s.handle(cmd, reason.Invalid(fmt.Sprintf("unexpected argument %q", cmd.Args().First())))
		}
		values, err := instructionText(topic)
		if err != nil {
			return s.handle(cmd, reason.Invalid(err.Error()))
		}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "instructions", map[string]any{"topic": topic, "instructions": values})
		}
		for _, value := range values {
			if _, err := fmt.Fprintln(s.writer, value); err != nil {
				return err
			}
		}
		return nil
	}
}

func instructionText(topic string) ([]string, error) {
	return instructions.For(topic)
}

func doctorAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if cmd.Args().Len() > 0 {
			return s.handle(cmd, reason.Invalid(fmt.Sprintf("unexpected argument %q", cmd.Args().First())))
		}
		selected, selectionErr := profileSelection(cmd)
		if selectionErr != nil {
			return s.handle(cmd, selectionErr)
		}
		if selected.Profile != nil {
			return remoteDoctorAction(s, ctx, cmd)
		}
		cfg, err := config.Load(config.Input{Flags: map[string]string{
			"home": cmd.String("home"), "config": cmd.String("config"),
		}})
		if err != nil {
			return s.handle(cmd, err)
		}
		cwd, cwdErr := currentWorkingDirectory()
		if cwdErr != nil {
			// Diagnose still emits every check when the process directory was
			// deleted; context.root becomes the single failing check.
			cwd = ""
		}
		checks := doctor.Diagnose(ctx, cfg, cwd)
		failed := false
		for _, check := range checks {
			if check.Status == "fail" {
				failed = true
				break
			}
		}
		if s.jsonRequested(cmd) {
			if failed {
				e := reason.New(reason.ReasonInternal, "doctor found failing checks").With("checks", checks)
				_ = output.WriteError(s.writer, "doctor", e)
				return &handledError{cause: e}
			}
			return output.WriteSuccess(s.writer, "doctor", map[string]any{"checks": checks})
		}
		if err := writeDoctorText(s.writer, checks, output.ColorEnabled(s.writer)); err != nil {
			return err
		}
		if failed {
			return reason.New(reason.ReasonInternal, "doctor found failing checks")
		}
		return nil
	}
}

var remoteDoctorProbeTimeout = 5 * time.Second

func remoteDoctorAction(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	resources := cmd.StringSlice("resource")
	if len(resources) > 1 {
		return s.handle(cmd, reason.Invalid("doctor accepts at most one resource"))
	}
	resource := ""
	if len(resources) == 1 {
		resource = strings.TrimSpace(resources[0])
		if resource == "" {
			return s.handle(cmd, reason.Invalid("doctor resource must not be blank"))
		}
	}
	backend, err := authorityFor(ctx, cmd, false)
	if err != nil {
		return s.handle(cmd, err)
	}
	defer backend.Close()
	checks := make([]doctor.Check, 0, 10)
	add := func(id, status, detail, hint string) {
		checks = append(checks, doctor.Check{ID: id, Status: status, Detail: output.RedactString(detail), Hint: output.RedactString(hint)})
	}
	profileName := backend.ProfileName
	add("remote.profile", "ok", "selected remote profile "+profileName, "")
	credentialOK := false
	credential := backend.Profile.Credential.Path
	info, statErr := os.Lstat(credential)
	switch {
	case os.IsNotExist(statErr):
		add("remote.credential", "fail", "installation credential is missing", "run worklease enroll --invite-file FILE to enroll this profile")
	case statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0:
		add("remote.credential", "fail", "installation credential path is unsafe", "replace it with an owner-private regular file using mode 0600")
	default:
		_, credentialErr := handle.ReadCredential(credential)
		if credentialErr == nil {
			credentialOK = true
			add("remote.credential", "ok", "installation credential is present in an owner-private file", "")
		} else if classified := reason.As(credentialErr); classified != nil && classified.Reason == reason.ReasonCredentialMalformed {
			add("remote.credential", "fail", "installation credential is malformed", "enroll again with worklease enroll --invite-file FILE")
		} else {
			add("remote.credential", "fail", "installation credential path is unsafe", "replace it with an owner-private regular file using mode 0600")
		}
	}

	metadataCtx, cancelMetadata := context.WithTimeout(ctx, remoteDoctorProbeTimeout)
	metadata, metadataErr := backend.HTTP.Metadata(metadataCtx)
	cancelMetadata()
	metadataOK := metadataErr == nil && backend.Profile.AuthorityID != "" && backend.Profile.RestoreID != "" && metadata.AuthorityID == backend.Profile.AuthorityID && metadata.RestoreID == backend.Profile.RestoreID
	if metadataErr != nil {
		appendMetadataFailure(add, backend, metadataErr)
	} else {
		add("remote.reachability", "ok", "remote endpoint answered within the diagnostic deadline", "")
		if backend.Profile.AllowInsecureHTTP {
			add("remote.tls", "warn", "profile explicitly allows insecure HTTP; TLS and a leaf pin are not verified", "enroll again with an HTTPS invite artifact for fail-closed transport trust")
		} else if backend.Profile.CertificateSHA256 != "" {
			add("remote.tls", "ok", "HTTPS leaf certificate matches the configured pin", "")
		} else {
			add("remote.tls", "ok", "HTTPS certificate validation succeeded using system trust", "")
		}
		add("remote.protocol", "ok", "server speaks worklease-http/1", "")
		if backend.Profile.AuthorityID == "" || backend.Profile.RestoreID == "" {
			add("remote.metadata", "fail", "selected profile does not pin both authority and restore identities", "enroll this profile again with worklease enroll --invite-file FILE")
		} else if metadata.AuthorityID != backend.Profile.AuthorityID || metadata.RestoreID != backend.Profile.RestoreID {
			add("remote.metadata", "fail", "remote authority or restore identity does not match the selected profile", "replace the profile only after independently verifying the authority identity or restore event")
		} else {
			add("remote.metadata", "ok", "authority and restore identities match the selected profile", "")
		}
	}

	if !metadataOK || metadata.Metadata == nil || metadata.Metadata.AdmittedPrefixes == nil {
		add("remote.prefixes", "warn", "admitted prefixes are not verified because the server did not provide them", "upgrade the remote authority and rerun worklease doctor")
	} else {
		prefixes := *metadata.Metadata.AdmittedPrefixes
		if resource == "" {
			add("remote.prefixes", "ok", "admitted prefixes: "+strings.Join(prefixes, ", "), "rerun with --resource KEY to verify one resource")
		} else if lease.ResourceAdmitted(prefixes, resource) {
			add("remote.prefixes", "ok", "the requested resource matches an admitted prefix", "")
		} else {
			add("remote.prefixes", "fail", "the requested resource is not admitted; configured prefixes: "+strings.Join(prefixes, ", "), "use a resource with an advertised prefix or update admittedPrefixes and restart worklease serve")
		}
	}

	role := ""
	if !credentialOK || !metadataOK {
		add("remote.authentication", "warn", "installation authentication was not verified because a prerequisite failed", "fix the failed prerequisite and rerun worklease doctor")
		add("remote.role", "warn", "current installation role was not verified", "fix the failed prerequisite and rerun worklease doctor")
	} else {
		selfCtx, cancelSelf := context.WithTimeout(ctx, remoteDoctorProbeTimeout)
		self, selfErr := backend.HTTP.InstallationSelf(selfCtx)
		cancelSelf()
		if classified := reason.As(selfErr); classified != nil && classified.Reason == reason.ReasonInvalidPath {
			listCtx, cancelList := context.WithTimeout(ctx, remoteDoctorProbeTimeout)
			_, listErr := backend.API.List(listCtx, "", nil)
			cancelList()
			if listErr == nil {
				add("remote.authentication", "ok", "installation authentication succeeded", "")
				add("remote.role", "warn", "current installation role is not verified because the server lacks self inspection", "upgrade the remote authority and rerun worklease doctor")
			} else {
				appendAuthenticationFailure(add, listErr)
				add("remote.role", "warn", "current installation role was not verified", "fix installation authentication and rerun worklease doctor")
			}
		} else if selfErr != nil {
			appendAuthenticationFailure(add, selfErr)
			add("remote.role", "warn", "current installation role was not verified", "fix installation authentication and rerun worklease doctor")
		} else {
			role = self.Role
			add("remote.authentication", "ok", "installation authentication succeeded", "")
			add("remote.role", "ok", "current installation role is "+role, "")
		}
	}

	if role != "admin" {
		add("remote.recovery", "warn", "recovery mode is not verified because the current role is not admin; this is not an onboarding failure", "ask an administrator to run worklease doctor if recovery status is needed")
	} else {
		recoveryCtx, cancelRecovery := context.WithTimeout(ctx, remoteDoctorProbeTimeout)
		raw, recoveryErr := adminCall(recoveryCtx, backend, "/v1/admin/recovery/status", "recovery-status", false, strings.Repeat("0", 32), map[string]any{})
		cancelRecovery()
		if recoveryErr != nil {
			add("remote.recovery", "fail", "administrative recovery status could not be verified", "check the authority logs and rerun worklease doctor")
		} else {
			var recovery struct {
				RecoveryMode bool `json:"recoveryMode"`
			}
			if json.Unmarshal(raw, &recovery) != nil {
				add("remote.recovery", "fail", "recovery status response is invalid", "upgrade or repair the remote authority")
			} else if recovery.RecoveryMode {
				add("remote.recovery", "warn", "remote authority is in recovery mode", "complete recovery reconciliation before admitting new work")
			} else {
				add("remote.recovery", "ok", "remote authority admission is open", "")
			}
		}
	}
	return finishRemoteDoctor(s, cmd, checks)
}

func appendMetadataFailure(add func(string, string, string, string), backend *authorityContext, err error) {
	kind := classifyRemoteProbeError(err)
	switch kind {
	case "dns":
		add("remote.reachability", "fail", "DNS lookup for the remote endpoint failed", "correct the selected profile endpoint or DNS configuration and rerun worklease doctor")
		add("remote.tls", "warn", "TLS and leaf pin were not verified because DNS failed", "fix reachability first")
	case "refused":
		add("remote.reachability", "fail", "the remote endpoint refused the connection", "check that worklease serve is listening and that routing or firewall policy permits the connection")
		add("remote.tls", "warn", "TLS and leaf pin were not verified because the connection was refused", "fix reachability first")
	case "timeout":
		add("remote.reachability", "fail", "the remote endpoint timed out within the diagnostic deadline", "check the listener, route, and firewall as possible causes; a timeout does not prove which one failed")
		add("remote.tls", "warn", "TLS and leaf pin were not verified before the deadline", "fix reachability first")
	case "tls":
		add("remote.reachability", "ok", "the remote host was reached", "")
		add("remote.tls", "fail", "TLS or configured leaf-certificate pin verification failed", "verify the endpoint hostname and securely replace the profile only if the server certificate intentionally changed")
	case "protocol", "identity":
		add("remote.reachability", "ok", "remote endpoint answered within the diagnostic deadline", "")
		if backend.Profile.AllowInsecureHTTP {
			add("remote.tls", "warn", "profile explicitly allows insecure HTTP; TLS and a leaf pin are not verified", "enroll again with an HTTPS invite artifact for fail-closed transport trust")
		} else if backend.Profile.CertificateSHA256 != "" {
			add("remote.tls", "ok", "HTTPS leaf certificate matches the configured pin", "")
		} else {
			add("remote.tls", "ok", "HTTPS certificate validation succeeded using system trust", "")
		}
	default:
		add("remote.reachability", "fail", "the remote endpoint could not be reached or decoded", "verify the selected profile endpoint and worklease serve status")
		add("remote.tls", "warn", "TLS and leaf pin were not verified", "fix reachability first")
	}
	status, detail, hint := "warn", "protocol was not verified because metadata failed", "fix transport reachability first"
	if kind == "protocol" {
		status, detail, hint = "fail", "remote protocol or response envelope is incompatible", "upgrade the client or remote authority to worklease-http/1"
	} else if kind == "identity" {
		status, detail, hint = "ok", "server speaks worklease-http/1", ""
	}
	add("remote.protocol", status, detail, hint)
	metadataDetail, metadataHint := "authority and restore identities were not verified", "fix the failed transport or protocol check first"
	if classified := reason.As(err); classified != nil && (classified.Reason == reason.ReasonAuthorityMismatch || classified.Reason == reason.ReasonAuthorityRestored) {
		metadataDetail, metadataHint = "remote authority identity does not match the selected profile", "replace the profile only after independently verifying the authority identity"
	}
	add("remote.metadata", "fail", metadataDetail, metadataHint)
}

func appendAuthenticationFailure(add func(string, string, string, string), err error) {
	detail := "installation authentication failed"
	hint := "enroll this profile again with worklease enroll --invite-file FILE"
	if classified := reason.As(err); classified != nil {
		switch classified.Reason {
		case reason.ReasonInstallationRevoked:
			detail = "installation credential is revoked"
			hint = "ask an administrator to issue a new invite, then enroll again"
		case reason.ReasonAuthorityRestored:
			detail = "installation belongs to an earlier authority restore incarnation"
			hint = "obtain a current invite from the restored authority and enroll again"
		}
	}
	add("remote.authentication", "fail", detail, hint)
}

func classifyRemoteProbeError(err error) string {
	if classified := reason.As(err); classified != nil {
		switch classified.Reason {
		case reason.ReasonRemoteTransportFailure:
			if transport, ok := classified.Details["transport"].(string); ok {
				switch transport {
				case "dns", "refused", "timeout", "tls":
					return transport
				}
			}
			return "connect"
		case reason.ReasonProtocolVersionUnsupported, reason.ReasonInvalidArgument:
			return "protocol"
		case reason.ReasonAuthorityMismatch:
			if strings.Contains(classified.Message, "certificate") {
				return "tls"
			}
			return "identity"
		case reason.ReasonAuthorityRestored:
			return "identity"
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var hostnameErr x509.HostnameError
	var authorityErr x509.UnknownAuthorityError
	var certificateErr x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	if errors.As(err, &hostnameErr) || errors.As(err, &authorityErr) || errors.As(err, &certificateErr) || errors.As(err, &recordErr) {
		return "tls"
	}
	return "connect"
}

func finishRemoteDoctor(s *boundary, cmd *urfave.Command, checks []doctor.Check) error {
	failed := false
	for _, check := range checks {
		failed = failed || check.Status == "fail"
	}
	if s.jsonRequested(cmd) {
		if failed {
			e := reason.New(reason.ReasonInternal, "doctor found failing checks").With("checks", checks)
			_ = output.WriteError(s.writer, "doctor", e)
			return &handledError{cause: e}
		}
		return output.WriteSuccess(s.writer, "doctor", map[string]any{"checks": checks})
	}
	outcome := "PASS"
	if failed {
		outcome = "FAIL"
	}
	if _, err := fmt.Fprintln(s.writer, outcome+" remote onboarding diagnostics"); err != nil {
		return err
	}
	if err := writeDoctorText(s.writer, checks, output.ColorEnabled(s.writer)); err != nil {
		return err
	}
	if failed {
		return reason.New(reason.ReasonInternal, "doctor found failing checks")
	}
	return nil
}

func currentWorkingDirectory() (string, error) {
	return os.Getwd()
}

func writeDoctorText(w io.Writer, checks []doctor.Check, color bool) error {
	for _, check := range checks {
		status := styledState(escapeDiagnosticText(check.Status), color)
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s", escapeDiagnosticText(check.ID), status, escapeDiagnosticText(check.Detail)); err != nil {
			return err
		}
		if strings.TrimSpace(check.Hint) != "" {
			if _, err := fmt.Fprintf(w, "\thint: %s", escapeDiagnosticText(check.Hint)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func escapeDiagnosticText(value string) string {
	if !utf8.ValidString(value) {
		return "[REDACTED]"
	}
	var escaped strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			escaped.WriteString(`\\n`)
		case '\r':
			escaped.WriteString(`\\r`)
		case '\t':
			escaped.WriteString(`\\t`)
		default:
			if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
				fmt.Fprintf(&escaped, `\\u%04x`, r)
			} else {
				escaped.WriteRune(r)
			}
		}
	}
	return escaped.String()
}
