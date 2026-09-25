package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

type queueAdapterApprovalPreview struct {
	SourceID         string `json:"sourceId"`
	Executable       string `json:"executable"`
	ExpectedAdapter  string `json:"expectedAdapterId"`
	ExpectedVersion  string `json:"expectedVersion"`
	SHA256           string `json:"sha256"`
	Approved         bool   `json:"approved"`
	ConfirmationFlag string `json:"confirmationFlag,omitempty"`
}

func queueAdapterRegistryKey(source config.QueueSource) string {
	if source.Adapter == "external" {
		return queue.ExternalSourceAdapterKey(source.ID)
	}
	return source.Adapter
}

func queueAdapterApprovalCommand(s *boundary) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "adapter",
		Usage: "inspect and explicitly approve external queue adapters",
		Commands: []*urfavecli.Command{queueAdapterCheckCommand(s), queueAdapterProtocolCommand(s), {
			Name:  "approve",
			Usage: "approve the configured executable for one external queue source",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "source", Usage: "explicit configured source `ID`"},
				&urfavecli.BoolFlag{Name: "acknowledge", Usage: "approve the displayed executable path, adapter ID, version, and SHA-256 digest"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				if !cmd.IsSet("source") || cmd.String("source") == "" {
					return s.handle(cmd, reason.Invalid("--source is required; adapter approval never selects a default source"))
				}
				cfg, err := config.LoadQueue(os.Getenv)
				if err != nil {
					return s.handle(cmd, err)
				}
				var source *config.QueueSource
				for i := range cfg.Sources {
					if cfg.Sources[i].ID == cmd.String("source") {
						source = &cfg.Sources[i]
						break
					}
				}
				if source == nil {
					return s.handle(cmd, reason.Invalid("unknown queue source: "+cmd.String("source")))
				}
				if source.Adapter != "external" {
					return s.handle(cmd, reason.Invalid("queue adapter approval requires an external source"))
				}
				digest, err := queueAdapterExecutableDigest(source.Executable)
				if err != nil {
					return s.handle(cmd, err)
				}
				preview := queueAdapterApprovalPreview{
					SourceID:         source.ID,
					Executable:       source.Executable,
					ExpectedAdapter:  source.ExpectedAdapterID,
					ExpectedVersion:  source.ExpectedVersion,
					SHA256:           digest,
					Approved:         false,
					ConfirmationFlag: "--acknowledge",
				}
				if cmd.Bool("acknowledge") {
					if err := config.ApproveQueueAdapter(ctx, os.Getenv, *source); err != nil {
						return s.handle(cmd, err)
					}
					if err := config.CheckQueueAdapterApproval(os.Getenv, *source); err != nil {
						return s.handle(cmd, err)
					}
					preview.Approved = true
					preview.ConfirmationFlag = ""
				}
				if cmd.Bool("json") {
					return output.WriteSuccess(s.writer, "queue-adapter-approval", map[string]any{"approval": preview})
				}
				if err := writeQueueAdapterApprovalPreview(s.writer, preview); err != nil {
					return err
				}
				if !preview.Approved {
					_, err = fmt.Fprintln(s.writer, "Not recorded. Re-run with --acknowledge to approve this executable.")
				}
				return err
			},
		}},
	}
}

// protocol reads a saved manifest, not an executable or an approved queue source.
func queueAdapterProtocolCommand(s *boundary) *urfavecli.Command {
	return &urfavecli.Command{
		Name: "protocol", Usage: "inspect host protocol majors and an optional saved adapter manifest without running it",
		UsageText: "worklease queue adapter protocol [--manifest-file FILE] [--json]",
		OnUsageError: func(_ context.Context, cmd *urfavecli.Command, _ error, _ bool) error {
			return adapterProtocolError(s, cmd, reason.Invalid("invalid adapter protocol arguments"))
		},
		Flags: []urfavecli.Flag{&urfavecli.StringFlag{Name: "manifest-file", Usage: "path to a saved JSON manifest (not an executable)"}},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			fail := func(err error) error { return adapterProtocolError(s, cmd, err) }
			if cmd.Args().Len() != 0 {
				return fail(reason.Invalid("unexpected positional arguments"))
			}
			majors := queue.SupportedExternalProtocolMajors()
			result := map[string]any{"hostProtocolMajors": majors}
			var adapterID, adapterVersion string
			var minMajor, maxMajor int
			if cmd.IsSet("manifest-file") {
				path := cmd.String("manifest-file")
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
					return fail(reason.Invalid("manifest file must be a readable JSON file under 1 MiB"))
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return fail(reason.Invalid("manifest file cannot be read"))
				}
				var manifest struct {
					ID       string `json:"id"`
					Version  string `json:"version"`
					Protocol struct {
						MinMajor int `json:"minMajor"`
						MaxMajor int `json:"maxMajor"`
					} `json:"protocol"`
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(data, &fields) != nil || fields == nil || fields["id"] == nil || fields["version"] == nil || fields["protocol"] == nil || json.Unmarshal(data, &manifest) != nil || !config.ValidQueueAdapterManifestIdentity(manifest.ID, manifest.Version) || manifest.Protocol.MinMajor < 1 || manifest.Protocol.MaxMajor < manifest.Protocol.MinMajor {
					return fail(reason.Invalid("manifest ID, version or protocol range is invalid"))
				}
				result["manifest"] = map[string]any{"id": manifest.ID, "version": manifest.Version, "protocol": manifest.Protocol}
				compatible := false
				for _, major := range majors {
					if major >= manifest.Protocol.MinMajor && major <= manifest.Protocol.MaxMajor {
						compatible = true
					}
				}
				result["protocolMajorsOverlap"] = compatible
				adapterID, adapterVersion = manifest.ID, manifest.Version
				minMajor, maxMajor = manifest.Protocol.MinMajor, manifest.Protocol.MaxMajor
			}
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "queue-adapter-protocol", result)
			}
			if _, err := fmt.Fprintf(s.writer, "Host protocol majors: %v\n", majors); err != nil {
				return err
			}
			if adapterID != "" {
				_, err := fmt.Fprintf(s.writer, "Adapter %s %s: majors %d–%d; protocol majors overlap: %t\n", adapterID, adapterVersion, minMajor, maxMajor, result["protocolMajorsOverlap"])
				return err
			}
			return nil
		},
	}
}

func adapterProtocolError(s *boundary, cmd *urfavecli.Command, err error) error {
	if !s.jsonRequested(cmd) {
		return err
	}
	if writeErr := output.WriteError(s.writer, "queue-adapter-protocol", err); writeErr != nil {
		return writeErr
	}
	return &handledError{cause: err}
}

// check is deliberately separate from approve: it neither loads queue.yaml nor
// records executable approval. It exercises an explicitly selected binary only.
func queueAdapterCheckCommand(s *boundary) *urfavecli.Command {
	return &urfavecli.Command{
		Name: "check", Usage: "check an external adapter through the production host",
		UsageText: "worklease queue adapter check --executable PATH [--adapter-config JSON | --adapter-config-file FILE] [--disposable-target ITEM] [--cancel-marker PATH] [--json]",
		OnUsageError: func(_ context.Context, cmd *urfavecli.Command, err error, _ bool) error {
			return adapterCheckError(s, cmd, reason.Invalid(err.Error()))
		},
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "executable", Usage: "absolute path to an adapter executable"},
			&urfavecli.StringFlag{Name: "adapter-config", Usage: "adapter source configuration as JSON object"},
			&urfavecli.StringFlag{Name: "adapter-config-file", Usage: "path to a JSON configuration object"},
			&urfavecli.StringFlag{Name: "disposable-target", Usage: "explicit disposable provider item ID for mutation probes"},
			&urfavecli.StringFlag{Name: "cancel-marker", Usage: "fresh fixture marker path; adapter writes .request on start and .done on cancellation"},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.String("executable") == "" || cmd.IsSet("adapter-config") && cmd.IsSet("adapter-config-file") || cmd.Args().Len() != 0 {
				return adapterCheckError(s, cmd, reason.Invalid("--executable is required; choose only one of --adapter-config and --adapter-config-file"))
			}
			var data []byte
			if file := cmd.String("adapter-config-file"); file != "" {
				info, err := os.Stat(file)
				if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
					return adapterCheckError(s, cmd, reason.Invalid("adapter configuration file must be a readable JSON file under 1 MiB"))
				}
				data, err = os.ReadFile(file)
				if err != nil {
					return adapterCheckError(s, cmd, reason.Invalid("adapter configuration file cannot be read"))
				}
			} else {
				data = []byte(cmd.String("adapter-config"))
			}
			configuration := map[string]any{}
			if len(data) > 0 && (len(data) > 1<<20 || json.Unmarshal(data, &configuration) != nil || configuration == nil) {
				return adapterCheckError(s, cmd, reason.Invalid("adapter configuration must be a JSON object under 1 MiB"))
			}
			report, err := queue.CheckExternalAdapter(ctx, queue.AdapterCheckOptions{Executable: cmd.String("executable"), Config: configuration, Target: cmd.String("disposable-target"), CancelMarker: cmd.String("cancel-marker")})
			if err != nil {
				return adapterCheckError(s, cmd, reason.Invalid("adapter check could not start the executable"))
			}
			if report.Verdict == "fail" {
				failure := reason.New(reason.ReasonAdapterConformanceFailed, "adapter conformance checks failed").With("verdict", report.Verdict).With("checks", report.Checks)
				if s.jsonRequested(cmd) {
					return adapterCheckError(s, cmd, failure)
				}
				writeAdapterCheckText(s.writer, report)
				return failure
			}
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "queue-adapter-check", map[string]any{"verdict": report.Verdict, "manifest": report.Manifest, "checks": report.Checks})
			}
			return writeAdapterCheckText(s.writer, report)
		},
	}
}

func adapterCheckError(s *boundary, cmd *urfavecli.Command, err error) error {
	if !s.jsonRequested(cmd) {
		return err
	}
	if writeErr := output.WriteError(s.writer, "queue-adapter-check", err); writeErr != nil {
		return writeErr
	}
	return &handledError{cause: err}
}

func writeAdapterCheckText(writer io.Writer, report queue.AdapterCheckReport) error {
	if _, err := fmt.Fprintf(writer, "Adapter conformance: %s\n", report.Verdict); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(writer, "%s: %s (%s) — %s\n", check.ID, check.Status, check.Reason, check.Detail); err != nil {
			return err
		}
	}
	return nil
}

func writeQueueAdapterApprovalPreview(writer io.Writer, preview queueAdapterApprovalPreview) error {
	if _, err := fmt.Fprintln(writer, "Queue adapter approval"); err != nil {
		return err
	}
	for _, field := range [][2]string{
		{"source", preview.SourceID},
		{"executable", preview.Executable},
		{"expected adapter ID", preview.ExpectedAdapter},
		{"expected version", preview.ExpectedVersion},
		{"SHA-256", preview.SHA256},
	} {
		if _, err := fmt.Fprintf(writer, "%s: %s\n", field[0], safeQueueCell(field[1])); err != nil {
			return err
		}
	}
	if preview.Approved {
		_, err := fmt.Fprintln(writer, "Approval recorded.")
		return err
	}
	return nil
}

func queueAdapterExecutableDigest(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", reason.Invalid("external adapter executable path must be absolute and canonical")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", reason.Invalid("external adapter executable must exist at its configured path without symlinks")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", reason.Invalid("external adapter executable cannot be read")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o111 == 0 {
		return "", reason.Invalid("external adapter executable must be a readable executable regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", reason.Invalid("external adapter executable cannot be hashed safely")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return "", reason.Invalid("external adapter executable changed while its digest was computed")
	}
	resolvedAfter, err := filepath.EvalSymlinks(path)
	current, statErr := os.Stat(path)
	if err != nil || resolvedAfter != path || statErr != nil || !os.SameFile(after, current) {
		return "", reason.Invalid("external adapter executable path changed while its digest was computed")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
