package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

type offlineHandleSelection struct {
	Path                string
	SelectorType        string
	SelectorSession     string
	SelectorProvenance  string
	SelectedAuthorityID string
}

func selectOfflineHandle(ctx context.Context, cmd *urfave.Command) (offlineHandleSelection, error) {
	cfg, err := configForCommand(cmd)
	if err != nil {
		return offlineHandleSelection{}, err
	}
	if explicit := strings.TrimSpace(first(cmd.String("handle"), os.Getenv("WORKLEASE_HANDLE"))); explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return offlineHandleSelection{}, reason.New(reason.ReasonHandleUnsafe, "handle path is unsafe")
		}
		authorityID, err := selectedOfflineAuthorityID(ctx, cmd, cfg)
		if err != nil {
			return offlineHandleSelection{}, err
		}
		return offlineHandleSelection{Path: filepath.Clean(path), SelectorType: "explicit-handle", SelectorProvenance: "unknown", SelectedAuthorityID: authorityID}, nil
	}
	authorityID, err := selectedOfflineAuthorityID(ctx, cmd, cfg)
	if err != nil {
		return offlineHandleSelection{}, err
	}
	if authorityID == "" {
		return offlineHandleSelection{}, reason.New(reason.ReasonHandleMalformed, "handle is missing")
	}
	root, err := handle.ContextRoot(mustGetwd(), nil)
	if err != nil {
		return offlineHandleSelection{}, err
	}
	path, err := handle.ResolveContextualPath(ctx, cfg.Home, root, cfg.SessionID, authorityID, false)
	if err != nil {
		return offlineHandleSelection{}, err
	}
	return offlineHandleSelection{Path: path, SelectorType: "contextual", SelectorSession: cfg.SessionID, SelectorProvenance: "resolved", SelectedAuthorityID: authorityID}, nil
}

func selectedOfflineAuthorityID(ctx context.Context, cmd *urfave.Command, cfg config.Config) (string, error) {
	selected, err := profileSelection(cmd)
	if err != nil {
		return "", err
	}
	if selected.Profile != nil {
		return selected.Profile.AuthorityID, nil
	}
	st, err := store.Open(ctx, cfg.Home, store.Options{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer st.Close()
	if st.Empty() {
		return "", nil
	}
	return st.AuthorityID(), nil
}

func readOfflineHandle(ctx context.Context, path string) (handle.Handle, error) {
	lock, err := handle.AcquireExistingLock(ctx, path+".lock")
	if err != nil {
		return handle.Handle{}, err
	}
	defer lock.Close()
	var firstRead handle.Handle
	if lock.Path() != "" {
		firstRead, err = lock.Read(path)
	} else {
		firstRead, err = handle.Read(path)
	}
	if err != nil {
		return handle.Handle{}, err
	}
	secondRead, err := handle.Read(path)
	if err != nil || !reflect.DeepEqual(firstRead, secondRead) {
		return handle.Handle{}, reason.New(reason.ReasonHandleInUse, "handle changed during inspection")
	}
	return firstRead, nil
}

func handleInspectionFields(selection offlineHandleSelection, h handle.Handle) map[string]any {
	fields := map[string]any{
		"handlePath":             selection.Path,
		"selectorType":           selection.SelectorType,
		"selectorProvenance":     selection.SelectorProvenance,
		"authorityId":            h.AuthorityID,
		"claimId":                h.ClaimID,
		"recordedState":          h.State,
		"recordedRevision":       h.Revision,
		"resources":              h.Resources,
		"agentId":                h.AgentID,
		"claimSessionId":         h.SessionID,
		"localReplaceAllowed":    h.LocalReplaceAllowed,
		"authorityStatus":        "not-contacted",
		"recordedExpiryIsProof":  false,
		"pendingRequestPresent":  h.PendingRequest != nil,
		"recoveryRequestPresent": h.RecoveryRequest != nil,
	}
	if selection.SelectorType == "contextual" {
		fields["selectorSession"] = selection.SelectorSession
	}
	if selection.SelectedAuthorityID != "" {
		fields["selectedAuthorityId"] = selection.SelectedAuthorityID
		fields["authorityMatchesSelection"] = selection.SelectedAuthorityID == h.AuthorityID
	}
	if !h.ExpiresAt.IsZero() {
		fields["recordedExpiresAt"] = h.ExpiresAt.UTC()
	}
	if !h.HoldUntil.IsZero() {
		fields["recordedHoldUntil"] = h.HoldUntil.UTC()
	}
	if h.AutoRenewOwner != "" {
		fields["autoRenewOwner"] = h.AutoRenewOwner
	}
	if pending := h.PendingRequest; pending != nil {
		fields["pendingKind"] = pending.Kind
		fields["pendingOperationId"] = pending.OperationID
		if !pending.RequestNotAfter.IsZero() {
			fields["pendingRequestNotAfter"] = pending.RequestNotAfter.UTC()
		}
	}
	if recovery := h.RecoveryRequest; recovery != nil {
		fields["recoveryOperationId"] = recovery.OperationID
		fields["recoveryTargetClaimId"] = recovery.TargetClaimID
		fields["recoveryTargetOperationId"] = recovery.TargetOperationID
		fields["recoveryOutcome"] = recovery.Outcome
		if !recovery.RequestNotAfter.IsZero() {
			fields["recoveryRequestNotAfter"] = recovery.RequestNotAfter.UTC()
		}
	}
	return fields
}

func writeOfflineHandleResult(s *boundary, cmd *urfave.Command, operation string, fields map[string]any) error {
	if s.jsonRequested(cmd) {
		return output.WritePublicSuccess(s.writer, operation, fields)
	}
	textFields := fields
	if fields["selectorType"] == "contextual" {
		textFields = make(map[string]any, len(fields))
		for key, value := range fields {
			textFields[key] = value
		}
		selector, _ := textFields["selectorSession"].(string)
		delete(textFields, "selectorSession")
		display := fmt.Sprintf("%q", selector)
		if selector == "" {
			display += " (unscoped)"
		}
		textFields["contextualHandleSelector"] = display
	}
	return output.WritePublicText(s.writer, operation, textFields)
}

func handleInspectAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		selection, err := selectOfflineHandle(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		h, err := readOfflineHandle(ctx, selection.Path)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeOfflineHandleResult(s, cmd, "handle inspect", handleInspectionFields(selection, h))
	}
}

func handleArchiveAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		selection, err := selectOfflineHandle(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		lock, err := handle.AcquireLock(ctx, selection.Path+".lock")
		if err != nil {
			return s.handle(cmd, err)
		}
		defer lock.Close()
		h, err := lock.Read(selection.Path)
		if err != nil {
			return s.handle(cmd, err)
		}
		needsAcknowledgement := h.State == "pending" || h.PendingRequest != nil || h.RecoveryRequest != nil
		if needsAcknowledgement && !cmd.Bool("acknowledge-pending-recovery") {
			return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "pending or recovery state requires --acknowledge-pending-recovery; the source handle was not changed"))
		}
		cfg, cfgErr := configForCommand(cmd)
		if cfgErr != nil {
			return s.handle(cmd, cfgErr)
		}
		destination := strings.TrimSpace(cmd.String("destination"))
		if destination == "" {
			directory := filepath.Join(cfg.Home, "handle-archive")
			if err = handle.EnsureOwnerPrivateDir(directory); err != nil {
				return s.handle(cmd, err)
			}
			destination = filepath.Join(directory, fmt.Sprintf("%s-%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"), h.ClaimID))
		} else {
			destination, err = filepath.Abs(destination)
			if err != nil {
				return s.handle(cmd, reason.New(reason.ReasonHandleUnsafe, "archive destination is unsafe"))
			}
		}
		destination = filepath.Clean(destination)
		if handle.InContextualDirectory(cfg.Home, destination) {
			return s.handle(cmd, reason.New(reason.ReasonHandleUnsafe, "archive destination cannot be in the contextual handle directory"))
		}
		if err = handle.ArchiveNoReplace(lock, selection.Path, destination, h); err != nil {
			return s.handle(cmd, err)
		}
		fields := map[string]any{
			"archivePath":          destination,
			"sourceHandlePath":     selection.Path,
			"authorityId":          h.AuthorityID,
			"claimId":              h.ClaimID,
			"recordedState":        h.State,
			"authorityMutation":    "none",
			"claimMayRemainActive": true,
			"recoveryCommand":      "worklease handle inspect --handle " + shellQuote(destination),
			"warning":              "the authority was not contacted; archiving does not release, revoke, or prove inactivity of the claim",
		}
		return writeOfflineHandleResult(s, cmd, "handle archive", fields)
	}
}
