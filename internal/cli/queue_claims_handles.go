package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/runs"
)

// claimsHandleIndex reads private handles locally. It never exposes tokens to
// the UI or sends credentials to the authority during discovery.
func claimsHandleIndex(home, authorityID, restoreID string, claims []lease.ClaimView, profileName ...string) map[string]queueui.ClaimsHandle {
	index := make(map[string]queueui.ClaimsHandle, len(claims))
	for _, claim := range claims {
		index[claim.ClaimID] = queueui.ClaimsHandle{Unavailable: "not held here; use worklease heartbeat|release --handle PATH or an explicit token file"}
	}
	queueOwned := make(map[string]bool)
	queueReadErr := filepath.WalkDir(filepath.Join(home, "queue-handles"), func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && path == filepath.Join(home, "queue-handles") {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		h, readErr := handle.Read(path)
		if readErr != nil {
			return readErr // unreadable queue ownership cannot authorize a Claims mutation
		}
		if h.AuthorityID == authorityID {
			queueOwned[h.ClaimID] = true
		}
		return nil
	})
	supervised := make(map[string]bool)
	records, runsErr := runStore().List()
	if runsErr == nil {
		for _, record := range records {
			if record.Claim != nil && record.Claim.AuthorityID == authorityID && !record.Terminal() && record.Health(time.Now(), runs.Alive) != "abandoned" {
				supervised[record.Claim.ClaimID] = true
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(home, "handles"))
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".json") || !(strings.HasPrefix(name, "ctx-") || strings.HasPrefix(name, "mcp-")) {
				continue
			}
			path := filepath.Join(home, "handles", name)
			h, readErr := handle.Read(path)
			if readErr != nil {
				continue
			}
			if _, exists := index[h.ClaimID]; !exists {
				continue
			}
			kind := "contextual"
			if strings.HasPrefix(name, "mcp-") {
				kind = "MCP"
			}
			candidate := queueui.ClaimsHandle{Kind: kind, Path: path, AuthorityID: h.AuthorityID, RestoreID: h.RestoreID, Revision: h.Revision}
			switch {
			case h.AuthorityID != authorityID:
				candidate.Unavailable = "handle authority mismatch; use worklease heartbeat|release --handle " + path
			case h.RestoreID != restoreID:
				candidate.Unavailable = "handle restore ID mismatch; use worklease heartbeat|release --handle " + path
			case queueOwned[h.ClaimID]:
				candidate.Unavailable = "queue-owned claim; use i / worklease queue for its managed lifecycle"
			case supervised[h.ClaimID]:
				candidate.Unavailable = "supervised run owns heartbeat and release; use worklease runs stop"
			case h.State != "ready" || h.PendingRequest != nil || h.RecoveryRequest != nil:
				candidate.Unavailable = "pending/recovery handle; " + claimsRecoveryCommand(h, path, home, profileName)
			case h.Revision != claimRevision(claims, h.ClaimID):
				candidate.Unavailable = "handle revision mismatch; use worklease heartbeat|release --handle " + path
			}
			previous := index[h.ClaimID]
			if previous.Path == "" || previous.Unavailable != "" && candidate.Unavailable == "" {
				index[h.ClaimID] = candidate
			}
		}
	}
	// Queue-owned and supervised claims take precedence even if their handle
	// lives outside the discoverable contextual/MCP directory.
	for _, claim := range claims {
		current := index[claim.ClaimID]
		if queueOwned[claim.ClaimID] {
			current.Unavailable = "queue-owned claim; use i / worklease queue for its managed lifecycle"
		} else if supervised[claim.ClaimID] {
			current.Unavailable = "supervised run owns heartbeat and release; use worklease runs stop"
		}
		if queueReadErr != nil || runsErr != nil || err != nil && !errors.Is(err, os.ErrNotExist) {
			current.Unavailable = "local handle ownership could not be checked"
		}
		if !claim.Active {
			current.Unavailable = "claim is inactive"
		}
		index[claim.ClaimID] = current
	}
	return index
}

func claimsRecoveryCommand(h handle.Handle, path, home string, profiles []string) string {
	flags := " --handle " + shellQuote(path) + " --home " + shellQuote(home)
	if len(profiles) > 0 && profiles[0] != "" && profiles[0] != config.LocalProfileName {
		flags += " --profile " + shellQuote(profiles[0])
	} else {
		flags += " --local"
	}
	if h.RecoveryRequest != nil || h.PendingRequest == nil || h.PendingRequest.Kind != "heartbeat" && h.PendingRequest.Kind != "release" {
		return "inspect the handle with worklease handle inspect" + flags + "; reconcile the pending operation in the CLI"
	}
	p := h.PendingRequest
	command := "worklease " + p.Kind + flags
	var remoteRequest struct {
		TTLMicros int64  `json:"ttlMicros"`
		Reason    string `json:"reason"`
	}
	if h.SchemaVersion == handle.RemoteSchemaVersion {
		_ = json.Unmarshal(p.Request, &remoteRequest)
	}
	if p.Kind == "heartbeat" {
		ttl := inputDuration(p.Inputs, "ttl", 0)
		if remoteRequest.TTLMicros > 0 {
			ttl = time.Duration(remoteRequest.TTLMicros) * time.Microsecond
		}
		if ttl > 0 {
			command += " --ttl " + ttl.String()
		}
	} else {
		releaseReason := inputString(p.Inputs, "reason")
		if remoteRequest.Reason != "" {
			releaseReason = remoteRequest.Reason
		}
		if releaseReason != "" && releaseReason != "released" {
			command += " --reason " + shellQuote(releaseReason)
		}
	}
	return "recover with " + command
}

func claimRevision(claims []lease.ClaimView, id string) int64 {
	for _, claim := range claims {
		if claim.ClaimID == id {
			return claim.Revision
		}
	}
	return 0
}

// mutateClaimsHandle rechecks every preview identity before passing a ready
// handle to the same lifecycle core used by heartbeat/release CLI commands.
func mutateClaimsHandle(ctx context.Context, backend *authorityContext, preview lease.ClaimView, selected queueui.ClaimsHandle, release bool, releaseReason string) (lease.ClaimView, error) {
	if selected.Path == "" || selected.Unavailable != "" {
		return lease.ClaimView{}, fmt.Errorf("handle unavailable: %s", selected.Unavailable)
	}
	if !strings.HasPrefix(selected.Path, filepath.Join(backend.Config.Home, "handles")+string(os.PathSeparator)) {
		return lease.ClaimView{}, errors.New("handle is not in the current Worklease home")
	}
	name := filepath.Base(selected.Path)
	if !strings.HasPrefix(name, "ctx-") && !strings.HasPrefix(name, "mcp-") {
		return lease.ClaimView{}, errors.New("handle is not contextual or MCP")
	}
	var lock *handle.Lock
	var err error
	if !backend.Remote {
		lock, err = handle.AcquireLock(ctx, selected.Path+".lock")
		if err != nil {
			return lease.ClaimView{}, err
		}
		defer lock.Close()
	}
	var current handle.Handle
	if lock == nil {
		current, err = handle.Read(selected.Path)
	} else {
		current, err = lock.Read(selected.Path)
	}
	if err != nil {
		return lease.ClaimView{}, fmt.Errorf("handle changed: %w", err)
	}
	if current.ClaimID != preview.ClaimID || current.ClaimID == "" {
		return lease.ClaimView{}, errors.New("claim ID changed since preview")
	}
	if current.Revision != selected.Revision || current.Revision != preview.Revision {
		return lease.ClaimView{}, errors.New("claim revision changed since preview")
	}
	if current.AuthorityID != selected.AuthorityID || current.AuthorityID != backend.AuthorityID() || preview.AuthorityID != backend.AuthorityID() {
		return lease.ClaimView{}, errors.New("selected authority changed since preview")
	}
	if current.RestoreID != selected.RestoreID || backend.Remote && (backend.Profile == nil || current.RestoreID != backend.Profile.RestoreID) || !backend.Remote && current.RestoreID != "" {
		return lease.ClaimView{}, errors.New("restore ID changed since preview")
	}
	if backend.Remote && backend.Profile != nil {
		profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
		if err != nil {
			return lease.ClaimView{}, fmt.Errorf("selected authority unavailable: %w", err)
		}
		profile, ok := profiles[backend.ProfileName]
		if !ok || profile.AuthorityID != backend.Profile.AuthorityID || profile.RestoreID != backend.Profile.RestoreID || profile.Endpoint != backend.Profile.Endpoint || profile.CertificateSHA256 != backend.Profile.CertificateSHA256 {
			return lease.ClaimView{}, errors.New("selected authority profile changed since preview")
		}
	}
	if current.State != "ready" || current.PendingRequest != nil || current.RecoveryRequest != nil {
		return lease.ClaimView{}, fmt.Errorf("pending/recovery handle; %s", claimsRecoveryCommand(current, selected.Path, backend.Config.Home, []string{backend.ProfileName}))
	}
	// Recheck queue and supervisor ownership under the local handle lock.
	rechecked := claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), current.RestoreID, []lease.ClaimView{preview}, backend.ProfileName)[preview.ClaimID]
	if rechecked.Unavailable != "" || rechecked.Path != selected.Path {
		return lease.ClaimView{}, fmt.Errorf("handle ownership changed: %s", rechecked.Unavailable)
	}
	now, err := claimsAuthorityNow(backend)
	if err != nil {
		return lease.ClaimView{}, err
	}
	status, err := backend.API.Status(ctx, lease.Selector{ClaimID: preview.ClaimID})
	if err != nil {
		return lease.ClaimView{}, err
	}
	if status.Claim == nil || !status.Claim.Active || !status.Claim.ExpiresAt.After(now) {
		return lease.ClaimView{}, errors.New("claim is no longer active or has expired on the authority clock")
	}
	if status.Claim.Revision != preview.Revision || status.Claim.Revision != current.Revision || status.Claim.AuthorityID != backend.AuthorityID() {
		return lease.ClaimView{}, errors.New("claim revision or authority changed since preview")
	}
	c := lease.Credentials{AuthorityID: current.AuthorityID, ClaimID: current.ClaimID, Token: current.Token, Revision: current.Revision, HandlePath: selected.Path}
	if backend.Remote {
		c.CredentialPath = backend.Profile.Credential.Path
	}
	deadline := now.Add(24 * time.Hour)
	if backend.Remote {
		deadline = time.Time{} // remote client supplies its clock-bound default
	}
	operation := randomHex(16)
	var receipt lease.Receipt
	if release {
		reasonText := strings.TrimSpace(releaseReason)
		if reasonText == "" {
			reasonText = "released"
		}
		if err := validatePublicCLI("release reason", reasonText, 1024, true); err != nil || strings.Contains(reasonText, current.Token) {
			return lease.ClaimView{}, reason.Invalid("release reason is invalid")
		}
		receipt, err = releaseHandle(ctx, backend.API, c, selected.Path, &current, lock, deadline, reasonText, operation)
	} else {
		receipt, err = heartbeatHandle(ctx, backend.API, c, selected.Path, &current, lock, deadline, backend.API.DefaultTTL(), operation)
	}
	if err != nil {
		if h, readErr := handle.Read(selected.Path); readErr == nil && (h.State == "pending" || h.PendingRequest != nil || h.RecoveryRequest != nil) {
			return lease.ClaimView{}, fmt.Errorf("outcome uncertain; %s: %w", claimsRecoveryCommand(h, selected.Path, backend.Config.Home, []string{backend.ProfileName}), err)
		}
		return lease.ClaimView{}, err
	}
	if release {
		return lease.ClaimView{}, nil
	}
	updated := *status.Claim
	updated.Revision = receipt.Revision
	updated.HeartbeatAt = now
	if value, ok := receipt.Result["expiresAt"].(string); ok {
		if expires, parseErr := time.Parse(time.RFC3339Nano, value); parseErr == nil {
			updated.ExpiresAt = expires
		}
	}
	return updated, nil
}

func claimsAuthorityNow(backend *authorityContext) (time.Time, error) {
	if backend.HTTP != nil {
		return backend.HTTP.Clock().UpperBound()
	}
	if backend.Local != nil {
		return backend.Local.AuthorityNow(), nil
	}
	return time.Time{}, errors.New("authority clock unavailable")
}
