package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/gc"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func gcAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		cutoffText := strings.TrimSpace(cmd.String("cutoff"))
		if cutoffText != "" && cmd.IsSet("retention-days") {
			return s.handle(cmd, reason.Invalid("retention-days and cutoff are mutually exclusive"))
		}
		cfg, err := config.Load(config.Input{Flags: map[string]string{
			"home": cmd.String("home"), "config": cmd.String("config"),
			"retention_days": func() string {
				if cmd.IsSet("retention-days") {
					return cmd.String("retention-days")
				}
				return ""
			}(),
		}})
		if err != nil {
			return s.handle(cmd, err)
		}
		now := time.Now().UTC()
		req := gc.Request{Apply: cmd.Bool("apply"), Now: now}
		if cutoffText != "" {
			cutoff, parseErr := time.Parse(time.RFC3339Nano, cutoffText)
			if parseErr != nil {
				return s.handle(cmd, reason.Invalid("cutoff must be RFC3339"))
			}
			req.Cutoff = cutoff
		} else {
			req.RetentionDays = cfg.RetentionDays
		}
		st, err := store.Open(ctx, cfg.Home, store.Options{ReadOnly: !req.Apply})
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		result, err := gc.New(st).Collect(ctx, req)
		if err != nil {
			return s.handle(cmd, err)
		}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "gc", map[string]any{"dryRun": result.DryRun, "capturedAt": result.CapturedAt, "cutoff": result.Cutoff, "retentionDays": result.RetentionDays, "eligible": result.Eligible, "protected": result.Protected, "retired": result.Retired, "collected": result.Collected, "prunedThroughSequence": result.PrunedThrough, "lastEventSequence": result.LastEventSequence})
		}
		return writeGCText(s.writer, result)
	}
}

func writeGCText(w io.Writer, result gc.Result) error {
	fields := map[string]any{"mode": "apply"}
	if result.DryRun {
		fields["mode"] = "preview"
	}
	fields["cutoff"] = result.Cutoff
	fields["eligible"] = summaryCountText(result.Eligible)
	fields["protected"] = summaryCountText(result.Protected)
	if len(result.Retired) > 0 {
		fields["retired"] = summaryCountText(result.Retired)
	}
	if len(result.Collected) > 0 {
		fields["collected"] = summaryCountText(result.Collected)
	}
	fields["prunedThroughSequence"] = result.PrunedThrough
	if result.DryRun {
		fields["hint"] = "re-run with --apply and this cutoff to collect"
	}
	return output.WriteText(w, "gc", fields)
}

func summaryCountText(values map[string]gc.Summary) string {
	parts := make([]string, 0, len(values))
	for _, key := range sortedStrings(summaryCounts(values)) {
		parts = append(parts, key+"="+fmt.Sprint(values[key].Count))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
func summaryCounts(values map[string]gc.Summary) map[string]int {
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[key] = value.Count
	}
	return out
}
