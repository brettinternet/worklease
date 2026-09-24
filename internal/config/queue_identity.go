package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/brettinternet/worklease/internal/handle"
)

// QueueIdentity records an operator-confirmed claim domain. It is deliberately
// separate from the disposable source index and from queue.yaml: changing a
// locator must not carry confirmation into a new exclusion domain.
type QueueIdentity struct {
	Adapter     string        `json:"adapter"`
	Locator     string        `json:"locator"`
	Policy      string        `json:"policy"`
	Source      string        `json:"source"`
	AuthorityID string        `json:"authorityId"`
	ItemIDs     []string      `json:"itemIds,omitempty"`
	Retired     []ClaimDomain `json:"retired,omitempty"`
}

// ClaimDomain is a former keyspace that new acquisitions must claim together
// with the current key until all legacy callers have been retired.
type ClaimDomain struct {
	Policy string `json:"policy"`
	Source string `json:"source"`
}

type QueueIdentities struct {
	Version int                      `json:"version"`
	Sources map[string]QueueIdentity `json:"sources"`
}

func QueueIdentityPath(env func(string) string) string {
	return filepath.Join(filepath.Dir(QueuePath(env)), "queue-identities.json")
}

func LoadQueueIdentities(env func(string) string) (QueueIdentities, error) {
	state := QueueIdentities{Version: 1, Sources: map[string]QueueIdentity{}}
	data, err := handle.ReadOwnerPrivate(QueueIdentityPath(env), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("queue identity record cannot be read safely: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 1 || state.Sources == nil {
		return QueueIdentities{}, fmt.Errorf("invalid queue identity record")
	}
	return state, nil
}

func SaveQueueIdentities(env func(string) string, state QueueIdentities) error {
	if state.Version != 1 || state.Sources == nil {
		return fmt.Errorf("invalid queue identity record")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return handle.WriteOwnerPrivate(QueueIdentityPath(env), data, 1<<20)
}

// UpdateQueueIdentities serializes read-modify-write updates across queue
// processes so a concurrent confirmation or item record is never lost.
func UpdateQueueIdentities(ctx context.Context, env func(string) string, update func(*QueueIdentities) error) error {
	lock, err := handle.AcquireLock(ctx, QueueIdentityPath(env)+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := LoadQueueIdentities(env)
	if err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}
	return SaveQueueIdentities(env, state)
}

// RecordQueueClaimItem adds an item ID to the confirmed claim domain before
// acquisition, so a later renumber of that item is checked against its old key.
func RecordQueueClaimItem(ctx context.Context, env func(string) string, sourceID string, expected QueueIdentity, itemID string) error {
	return UpdateQueueIdentities(ctx, env, func(state *QueueIdentities) error {
		current, ok := state.Sources[sourceID]
		if !ok || current.Adapter != expected.Adapter || current.Locator != expected.Locator || current.Policy != expected.Policy || current.Source != expected.Source || current.AuthorityID != expected.AuthorityID {
			return fmt.Errorf("queue identity changed during acquisition")
		}
		if slices.Contains(current.ItemIDs, itemID) {
			return nil
		}
		current.ItemIDs = append(current.ItemIDs, itemID)
		slices.Sort(current.ItemIDs)
		state.Sources[sourceID] = current
		return nil
	})
}
