package cli

import (
	"context"
	"encoding/json"
	"strings"

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
			event, projectionErr := watchEventProjection(result.Event)
			if projectionErr != nil {
				return s.handle(cmd, reason.New(reason.ReasonInternal, "project watch event"))
			}
			return output.WriteSuccess(s.writer, "watch", map[string]any{
				"authorityId": result.AuthorityID, "cursor": result.Cursor, "nextCursor": result.NextCursor,
				"event": event, "timedOut": result.TimedOut, "gap": result.Gap,
				"resetCursor": result.ResetCursor, "free": result.Free, "changed": result.Changed,
				"resources": result.Resources, "unresolvedPredecessor": result.UnresolvedPredecessor,
				"unresolvedOperations": result.UnresolvedOperations,
			})
		}
		return writeWatchText(s.writer, result)
	}
}

func storeForWatch(ctx context.Context, home string) (*store.Store, error) {
	return store.Open(ctx, home, store.Options{ReadOnly: true})
}

// watchEventProjection converts the typed event into JSON-shaped maps and
// slices so output's recursive credential redaction reaches every public field.
func watchEventProjection(event *ledger.Event) (any, error) {
	if event == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var projected map[string]any
	if err := json.Unmarshal(encoded, &projected); err != nil {
		return nil, err
	}
	return projected, nil
}

func writeWatchText(w interface{ Write([]byte) (int, error) }, result watchpkg.Result) error {
	fields := map[string]any{"nextCursor": result.NextCursor, "timedOut": result.TimedOut, "gap": result.Gap}
	if result.Event != nil {
		fields["event"] = result.Event.Kind
	}
	if result.Free {
		fields["free"] = true
	}
	if result.Changed {
		fields["changed"] = true
	}
	if len(result.UnresolvedPredecessor) > 0 {
		fields["unresolvedPredecessor"] = result.UnresolvedPredecessor
	}
	if len(result.UnresolvedOperations) > 0 {
		fields["unresolvedOperations"] = strings.Join(result.UnresolvedOperations, ",")
	}
	return output.WriteText(w, "watch", fields)
}
