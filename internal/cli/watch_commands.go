package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	watchpkg "github.com/brettinternet/worklease/internal/watch"
	urfave "github.com/urfave/cli/v3"
)

func watchAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		cursor := strings.TrimSpace(cmd.String("cursor"))
		// Structural cursor validation deliberately precedes config and storage
		// resolution: malformed input must not create or open an authority.
		if cursor != "" {
			if _, err := ledger.ParseCursor(cursor); err != nil {
				return s.handle(cmd, err)
			}
		}
		resources := cmd.StringSlice("resource")
		if err := watchpkg.ValidateResources(resources); err != nil {
			return s.handle(cmd, err)
		}
		until := strings.TrimSpace(strings.ToLower(cmd.String("until")))
		if until != "" && until != "free" && until != "change" {
			return s.handle(cmd, reason.Invalid("until must be free or change"))
		}
		if until != "" && len(resources) == 0 {
			return s.handle(cmd, reason.Invalid("until requires at least one resource"))
		}
		if cursor == "" && until == "" {
			return s.handle(cmd, reason.Invalid("watch requires a cursor or resources with --until"))
		}
		timeout := cmd.Duration("timeout")
		if timeout < 0 || timeout > watchpkg.MaxTimeout {
			return s.handle(cmd, reason.Invalid("timeout must be between 1s and 1h"))
		}
		cfg, err := config.Load(config.Input{Flags: map[string]string{
			"home": cmd.String("home"), "config": cmd.String("config"),
			"poll_interval": cmd.String("poll-interval"),
		}})
		if err != nil {
			return s.handle(cmd, err)
		}
		st, err := storeForWatch(ctx, cfg.Home)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		result, err := watchpkg.Wait(ctx, st, watchpkg.Request{Cursor: cursor, Resources: resources, Until: until, Timeout: timeout, PollInterval: cfg.PollInterval})
		if err != nil {
			return s.handle(cmd, err)
		}
		if s.jsonRequested(cmd) {
			return output.WritePublicSuccess(s.writer, "watch", map[string]any{
				"authorityId": result.AuthorityID, "cursor": result.Cursor, "nextCursor": result.NextCursor,
				"event": result.Event, "timedOut": result.TimedOut, "gap": result.Gap,
				"resetCursor": result.ResetCursor, "free": result.Free, "changed": result.Changed,
				"resources": result.Resources, "unresolvedPredecessor": result.UnresolvedPredecessor,
				"unresolvedOperations": result.UnresolvedOperations,
			})
		}
		return writeWatchText(s.writer, result, output.ColorEnabled(s.writer))
	}
}

func writeWatchText(w io.Writer, result watchpkg.Result, color bool) error {
	return writeWatchTextAt(w, result, color, time.Now())
}

func storeForWatch(ctx context.Context, home string) (*store.Store, error) {
	return store.Open(ctx, home, store.Options{ReadOnly: true})
}

// writeWatchTextAt keeps the compact view relative and presents the durable
// resumption cursor only as a copyable command, never as a bare value.
func writeWatchTextAt(w io.Writer, result watchpkg.Result, color bool, now time.Time) error {
	title := "lifecycle state observed"
	if result.TimedOut {
		title = "watch timed out"
	} else if result.Event != nil {
		title = "observed " + styledState(result.Event.Kind, color) + " event"
	} else if result.Free {
		title = "resources are " + styledState("free", color)
	} else if result.Changed {
		title = "resources changed"
	}
	lines := []string{
		fmt.Sprintf("timedOut: %t", result.TimedOut),
		fmt.Sprintf("gap: %t", result.Gap),
	}
	if len(result.Resources) > 0 {
		states := make([]string, 0, len(result.Resources))
		expires := false
		for _, state := range result.Resources {
			value := summarizeResource(state.Resource) + "=" + styledState(state.State, color)
			if !state.ExpiresAt.IsZero() {
				expires = true
				value += " (expires " + relativeExpiry(state.ExpiresAt, now) + ")"
			}
			states = append(states, value)
		}
		lines = append(lines, "resources: "+strings.Join(states, ", "))
		if expires {
			lines = append(lines, "hint: watch rechecks state at the nearest expiry; verify ownership before mutating")
		}
	}
	if len(result.UnresolvedPredecessor) > 0 {
		predecessors := make([]string, 0, len(result.UnresolvedPredecessor))
		for _, predecessor := range result.UnresolvedPredecessor {
			resources := make([]string, len(predecessor.Resources))
			for i, value := range predecessor.Resources {
				resources[i] = resourceText(value)
			}
			predecessors = append(predecessors, fmt.Sprintf("claimId=%s operationId=%s resources=%s", escapeTerminalCell(predecessor.ClaimID), escapeTerminalCell(predecessor.OperationID), strings.Join(resources, ",")))
		}
		lines = append(lines, "unresolvedPredecessor: "+strings.Join(predecessors, "; "))
	}
	if len(result.UnresolvedOperations) > 0 {
		escaped := make([]string, len(result.UnresolvedOperations))
		for i, operationID := range result.UnresolvedOperations {
			escaped[i] = escapeTerminalCell(operationID)
		}
		lines = append(lines, "unresolvedOperations: "+strings.Join(escaped, ","))
	}
	if result.NextCursor != "" {
		lines = append(lines, "resume: worklease watch --cursor "+escapeTerminalCell(result.NextCursor))
	}
	return writeLines(w, title, lines)
}

func relativeExpiry(expiresAt, now time.Time) string {
	delta := expiresAt.Sub(now)
	if delta <= 0 {
		return relativeDuration(-delta) + " ago"
	}
	return "in " + relativeDuration(delta)
}
