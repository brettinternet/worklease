package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	tea "github.com/charmbracelet/bubbletea"
	urfave "github.com/urfave/cli/v3"
)

func queueClaimsOnlyFallback(cfg config.QueueConfig, loadErr error) (string, bool, error) {
	if loadErr != nil {
		if classified := reason.As(loadErr); classified != nil && classified.Reason == reason.ReasonNoSourcesConfigured {
			return "queue.yaml is not configured; showing authority-wide claims only", true, nil
		}
		return "", false, loadErr
	}
	if len(cfg.Views) == 0 {
		return "queue.yaml has no views; showing authority-wide claims only", true, nil
	}
	return "", false, nil
}

func queueClaimsCommand(s *boundary) *urfave.Command {
	command := &urfave.Command{
		Name:      "claims",
		Usage:     "browse current authority claims in the queue TUI",
		UsageText: "worklease queue claims [--high-contrast]",
		Description: "Open the queue TUI directly on the read-only, authority-wide Claims tab. " +
			"This view lists claims independently of loaded queue items.\n\nExamples:\n  worklease queue claims",
	}
	command.Action = func(ctx context.Context, cmd *urfave.Command) error {
		if s.jsonRequested(cmd) {
			return s.handle(cmd, reason.Invalid("queue TUI is text-only"))
		}
		cfg, loadErr := config.LoadQueue(os.Getenv)
		notice, _, err := queueClaimsOnlyFallback(cfg, loadErr)
		if err != nil {
			return s.handle(cmd, err)
		}
		return s.handle(cmd, runQueueClaimsOnly(ctx, cmd, s, notice))
	}
	return command
}

func runQueueClaimsOnly(ctx context.Context, cmd *urfave.Command, s *boundary, notice string) error {
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
	model := newClaimsOnlyModel(fmt.Sprintf("%s %s", backend.ProfileName, backend.AuthorityID()), scope, cmd.Bool("high-contrast"), notice)
	configureClaimsTab(&model, backend, ctx, backend.Config.SessionID)
	program := tea.NewProgram(model, tea.WithOutput(s.writer), tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = program.Run()
	return err
}

func newClaimsOnlyModel(authority, scope string, highContrast bool, notice string) queueui.Model {
	model := queueui.New(queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}})
	model.Views = []string{queueui.ClaimsViewID}
	model.ViewName = queueui.ClaimsViewID
	model.HighContrast = highContrast
	model.Authority = authority
	model.Scope = scope
	model.Claims.Loading = true
	model.Claims.Notice = notice
	return model
}

func configureClaimsTab(model *queueui.Model, backend *authorityContext, ctx context.Context, session string) {
	model.Claims.MineAgentID = backend.Config.AgentID
	model.Claims.MineSessionID = session
	model.Claims.PublicOnly = backend.Remote
	model.Claims.Refresh = func(cursor string) tea.Cmd {
		return queueui.ClaimsRefreshCmd(ctx, backend.API, cursor)
	}
	model.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
		return queueui.ClaimHistoryCmd(ctx, backend.API, claimID, resource, cursor)
	}
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
