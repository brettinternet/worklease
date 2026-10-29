package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/brettinternet/worklease/internal/claimsui"
	"github.com/brettinternet/worklease/internal/lease"
	tea "github.com/charmbracelet/bubbletea"
	urfave "github.com/urfave/cli/v3"
)

func runClaims(ctx context.Context, cmd *urfave.Command, s *boundary) error {
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
	model := newClaimsOnlyModel(fmt.Sprintf("%s %s", backend.ProfileName, backend.AuthorityID()), scope, false)
	configureClaims(&model, backend, ctx, backend.Config.SessionID)
	if !backend.Remote {
		// A fresh home has no database to hold open. Reopen for each read so
		// claims created in another process become visible without restarting.
		model.Claims.Refresh = func(cursor string) tea.Cmd {
			return func() tea.Msg {
				reader, err := authorityFor(ctx, cmd, false)
				if err != nil {
					return claimsui.ClaimsRefreshMsg{ClaimsErr: err, EventsErr: err}
				}
				defer reader.Close()
				return claimsui.ClaimsRefreshCmd(ctx, reader.API, cursor)()
			}
		}
		model.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
			return func() tea.Msg {
				reader, err := authorityFor(ctx, cmd, false)
				if err != nil {
					return claimsui.ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Err: err}
				}
				defer reader.Close()
				return claimsui.ClaimHistoryCmd(ctx, reader.API, claimID, resource, cursor)()
			}
		}
		model.Claims.ResolveHandles = func(claims []lease.ClaimView) map[string]claimsui.ClaimsHandle {
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
	model.Claims.Mutate = func(actionCtx context.Context, claim lease.ClaimView, selected claimsui.ClaimsHandle, reasonText string, release bool) (lease.ClaimView, error) {
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

func newClaimsOnlyModel(authority, scope string, highContrast bool) claimsui.Model {
	model := claimsui.New()
	model.HighContrast = highContrast
	model.Authority = authority
	model.Scope = scope
	model.Claims.Loading = true
	return model
}

func configureClaims(model *claimsui.Model, backend *authorityContext, ctx context.Context, session string) {
	model.Claims.MineAgentID = backend.Config.AgentID
	model.Claims.MineSessionID = session
	model.Claims.PublicOnly = backend.Remote
	model.Claims.Loading = true // Init performs the first read
	model.Claims.Refresh = func(cursor string) tea.Cmd {
		return claimsui.ClaimsRefreshCmd(ctx, backend.API, cursor)
	}
	model.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
		return claimsui.ClaimHistoryCmd(ctx, backend.API, claimID, resource, cursor)
	}
	model.Claims.LoadContact = func(claimID, sessionID, agentID string) tea.Cmd {
		return claimsui.ClaimContactCmd(ctx, claimID, sessionID, agentID)
	}
	model.Claims.ResolveHandles = func(claims []lease.ClaimView) map[string]claimsui.ClaimsHandle {
		restoreID := ""
		if backend.Profile != nil {
			restoreID = backend.Profile.RestoreID
		}
		return claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), restoreID, claims, backend.ProfileName)
	}
	model.Claims.Mutate = func(actionCtx context.Context, claim lease.ClaimView, selected claimsui.ClaimsHandle, reasonText string, release bool) (lease.ClaimView, error) {
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
