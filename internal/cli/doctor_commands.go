package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/doctor"
	"github.com/brettinternet/worklease/internal/instructions"
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

func remoteDoctorAction(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	backend, err := authorityFor(ctx, cmd, false)
	if err != nil {
		return s.handle(cmd, err)
	}
	defer backend.Close()
	checks := []doctor.Check{}
	credential := backend.Profile.Credential.Path
	if info, statErr := os.Lstat(credential); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0 {
		checks = append(checks, doctor.Check{ID: "remote.credential", Status: "ok", Detail: "installation credential is present in an owner-private file"})
	} else if os.IsNotExist(statErr) {
		checks = append(checks, doctor.Check{ID: "remote.credential", Status: "fail", Detail: "installation credential is missing"})
	} else {
		checks = append(checks, doctor.Check{ID: "remote.credential", Status: "fail", Detail: "installation credential path is unsafe"})
	}
	metadata, metadataErr := backend.HTTP.Metadata(ctx)
	if metadataErr != nil {
		checks = append(checks, doctor.Check{ID: "remote.metadata", Status: "fail", Detail: "remote authority metadata is unreachable"})
	} else {
		checks = append(checks, doctor.Check{ID: "remote.metadata", Status: "ok", Detail: "authority " + metadata.AuthorityID + " restore " + metadata.RestoreID})
	}
	if _, authErr := backend.API.List(ctx, "", nil); authErr != nil {
		checks = append(checks, doctor.Check{ID: "remote.authentication", Status: "fail", Detail: "authenticated authority check failed"})
	} else {
		checks = append(checks, doctor.Check{ID: "remote.authentication", Status: "ok", Detail: "installation authentication succeeded"})
	}
	if raw, recoveryErr := adminCall(ctx, backend, "/v1/admin/recovery/status", "recovery-status", false, strings.Repeat("0", 32), map[string]any{}); recoveryErr != nil {
		status := "fail"
		if classified := reason.As(recoveryErr); classified != nil && classified.Reason == reason.ReasonAuthorizationDenied {
			status = "warn"
		}
		checks = append(checks, doctor.Check{ID: "remote.recovery", Status: status, Detail: "recovery status requires an administrative installation"})
	} else {
		var recovery struct {
			RecoveryMode bool `json:"recoveryMode"`
		}
		if json.Unmarshal(raw, &recovery) != nil {
			checks = append(checks, doctor.Check{ID: "remote.recovery", Status: "fail", Detail: "recovery status response is invalid"})
		} else if recovery.RecoveryMode {
			checks = append(checks, doctor.Check{ID: "remote.recovery", Status: "warn", Detail: "remote authority is in recovery mode"})
		} else {
			checks = append(checks, doctor.Check{ID: "remote.recovery", Status: "ok", Detail: "remote authority admission is open"})
		}
	}
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
