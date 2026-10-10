package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	tea "github.com/charmbracelet/bubbletea"
	urfave "github.com/urfave/cli/v3"
)

func queueClaimsOnlyFallback(cfg config.QueueConfig, loadErr error) (bool, error) {
	if loadErr != nil {
		if classified := reason.As(loadErr); classified != nil && classified.Reason == reason.ReasonNoSourcesConfigured {
			return true, nil
		}
		return false, loadErr
	}
	return len(cfg.Views) == 0, nil
}

func queueClaimsCommand(s *boundary) *urfave.Command {
	command := &urfave.Command{
		Name:      "claims",
		Usage:     "browse current authority claims in the TUI",
		UsageText: "worklease queue claims [--high-contrast]",
		Description: "Open the TUI directly on the authority-wide Claims tab. Eligible locally held claims can be renewed or released. " +
			"This view lists claims independently of loaded queue items.\n\nExamples:\n  worklease queue claims",
	}
	command.Action = func(ctx context.Context, cmd *urfave.Command) error {
		if s.jsonRequested(cmd) {
			return s.handle(cmd, reason.Invalid("queue TUI is text-only"))
		}
		cfg, loadErr := config.LoadQueue(os.Getenv)
		_, err := queueClaimsOnlyFallback(cfg, loadErr)
		if err != nil {
			return s.handle(cmd, err)
		}
		return s.handle(cmd, runQueueClaimsOnly(ctx, cmd, s))
	}
	return command
}

func runQueueClaimsOnly(ctx context.Context, cmd *urfave.Command, s *boundary) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	backend, err := authorityFor(ctx, cmd, false)
	if err != nil {
		return err
	}
	defer backend.Close()
	scope := "local"
	if backend.Remote {
		scope = "remote"
	}
	model := newClaimsOnlyModel(fmt.Sprintf("%s %s", backend.ProfileName, backend.AuthorityID()), scope, cmd.Bool("high-contrast"))
	configureClaimsTab(&model, backend, ctx, backend.Config.SessionID)
	if !backend.Remote {
		// A fresh home has no database to hold open. Reopen for each read so
		// claims created in another process become visible without restarting.
		model.Claims.Refresh = func(cursor string) tea.Cmd {
			return func() tea.Msg {
				reader, err := authorityFor(ctx, cmd, false)
				if err != nil {
					return queueui.ClaimsRefreshMsg{ClaimsErr: err, EventsErr: err}
				}
				defer reader.Close()
				return queueui.ClaimsRefreshCmd(ctx, reader.API, cursor)()
			}
		}
		model.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
			return func() tea.Msg {
				reader, err := authorityFor(ctx, cmd, false)
				if err != nil {
					return queueui.ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Err: err}
				}
				defer reader.Close()
				return queueui.ClaimHistoryCmd(ctx, reader.API, claimID, resource, cursor)()
			}
		}
		model.Claims.ResolveHandles = func(claims []lease.ClaimView) map[string]queueui.ClaimsHandle {
			reader, err := authorityFor(ctx, cmd, false)
			if err != nil {
				return nil
			}
			defer reader.Close()
			return claimsHandleIndex(reader.Config.Home, reader.AuthorityID(), "", claims, reader.ProfileName)
		}
	}
	// Browsing must not create a local store in a fresh home. Open a writable
	// authority only after confirmation, and reselect it at dispatch time.
	model.Claims.Mutate = func(actionCtx context.Context, claim lease.ClaimView, selected queueui.ClaimsHandle, reasonText string, release bool) (lease.ClaimView, error) {
		writer, err := authorityFor(actionCtx, cmd, true)
		if err != nil {
			return lease.ClaimView{}, err
		}
		defer writer.Close()
		return mutateClaimsHandle(actionCtx, writer, claim, selected, release, reasonText)
	}
	program := tea.NewProgram(model, tea.WithOutput(s.writer), tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = program.Run()
	return err
}

func newClaimsOnlyModel(authority, scope string, highContrast bool) queueui.Model {
	model := queueui.New(queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}})
	model.Views = []string{queueui.ClaimsViewID}
	model.ViewName = queueui.ClaimsViewID
	model.HighContrast = highContrast
	model.Authority = authority
	model.Scope = scope
	model.Claims.Loading = true
	return model
}

func configureClaimsTab(model *queueui.Model, backend *authorityContext, ctx context.Context, session string) {
	model.Claims.MineAgentID = backend.Config.AgentID
	model.Claims.MineSessionID = session
	model.Claims.PublicOnly = backend.Remote
	model.Claims.Loading = true // Init performs the first read
	model.Claims.Refresh = func(cursor string) tea.Cmd {
		return queueui.ClaimsRefreshCmd(ctx, backend.API, cursor)
	}
	model.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
		return queueui.ClaimHistoryCmd(ctx, backend.API, claimID, resource, cursor)
	}
	model.Claims.ResolveHandles = func(claims []lease.ClaimView) map[string]queueui.ClaimsHandle {
		restoreID := ""
		if backend.Profile != nil {
			restoreID = backend.Profile.RestoreID
		}
		return claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), restoreID, claims, backend.ProfileName)
	}
	model.Claims.Mutate = func(actionCtx context.Context, claim lease.ClaimView, selected queueui.ClaimsHandle, reasonText string, release bool) (lease.ClaimView, error) {
		return mutateClaimsHandle(actionCtx, backend, claim, selected, release, reasonText)
	}
	model.Claims.RenewTTL = backend.API.DefaultTTL()
	model.Claims.Now = func() time.Time {
		if backend.HTTP != nil {
			if now, err := backend.HTTP.Clock().UpperBound(); err == nil {
				return now
			}
		}
		if backend.Local != nil {
			return backend.Local.AuthorityNow()
		}
		return time.Now().UTC()
	}
}
