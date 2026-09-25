package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		Commands: []*urfavecli.Command{{
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
