// Package watch implements bounded, read-only lifecycle observations.
package watch

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	resourcepkg "github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
)

const (
	DefaultTimeout = 30 * time.Second
	MaxTimeout     = time.Hour // Bounds polling cost and orphan lifetime; longer waits resume from NextCursor.
	DefaultPoll    = 250 * time.Millisecond
	MinPoll        = 50 * time.Millisecond
	MaxPoll        = 500 * time.Millisecond
)

type Clock interface{ Now() time.Time }
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

type Request struct {
	Cursor       string
	Resources    []string
	Until        string
	Timeout      time.Duration
	PollInterval time.Duration
	Clock        Clock
}

type ResourceState struct {
	Resource  string    `json:"resource"`
	State     string    `json:"state"`
	ClaimID   string    `json:"claimId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type Predecessor struct {
	ClaimID     string   `json:"claimId"`
	OperationID string   `json:"operationId"`
	Resources   []string `json:"resources"`
}

type Result struct {
	AuthorityID           string          `json:"authorityId"`
	Cursor                string          `json:"cursor"`
	NextCursor            string          `json:"nextCursor"`
	Event                 *ledger.Event   `json:"event,omitempty"`
	TimedOut              bool            `json:"timedOut"`
	Gap                   bool            `json:"gap"`
	ResetCursor           string          `json:"resetCursor,omitempty"`
	Free                  bool            `json:"free,omitempty"`
	Changed               bool            `json:"changed,omitempty"`
	Resources             []ResourceState `json:"resources,omitempty"`
	UnresolvedPredecessor []Predecessor   `json:"unresolvedPredecessor,omitempty"`
	UnresolvedOperations  []string        `json:"unresolvedOperations,omitempty"`
}

// Wait performs short independent read transactions until an event or state
// transition is observed. It never writes to the authority or holds a
// transaction while sleeping.
// ValidateResources applies the canonical resource-key validation to watch
// filters before any authority is opened.
func ValidateResources(resources []string) error {
	if len(resources) > 32 {
		return reason.New(reason.ReasonInvalidResource, "watch requires at most 32 resources")
	}
	seen := map[string]bool{}
	for _, value := range resources {
		if seen[value] {
			return reason.Invalid("watch resources must be non-empty and unique")
		}
		if _, err := resourcepkg.Direct(value, false); err != nil {
			return err
		}
		seen[value] = true
	}
	return nil
}

func Wait(ctx context.Context, st *store.Store, req Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("nil context")
	}
	if st == nil {
		return Result{}, errors.New("nil store")
	}
	if err := ValidateResources(req.Resources); err != nil {
		return Result{}, err
	}
	if req.Cursor != "" {
		if _, err := ledger.ParseCursor(req.Cursor); err != nil {
			return Result{}, err
		}
	}
	if req.Cursor == "" && req.Until == "" {
		return Result{}, reason.Invalid("watch requires a cursor or resources with --until")
	}
	if req.Until != "" && req.Until != "free" && req.Until != "change" {
		return Result{}, reason.Invalid("until must be free or change")
	}
	if req.Until != "" && len(req.Resources) == 0 {
		return Result{}, reason.Invalid("until requires at least one resource")
	}
	timeout, err := normalizeTimeout(req.Timeout)
	if err != nil {
		return Result{}, err
	}
	poll := req.PollInterval
	if poll == 0 {
		poll = DefaultPoll
	}
	if poll < MinPoll {
		poll = MinPoll
	}
	if poll > MaxPoll {
		poll = MaxPoll
	}
	clock := req.Clock
	if clock == nil {
		clock = wallClock{}
	}
	filter := ledger.ResourcesFilter(req.Resources)
	if req.Cursor != "" {
		cursor, _ := ledger.ParseCursor(req.Cursor)
		if st.AuthorityID() == "" || cursor.AuthorityID != st.AuthorityID() || cursor.Feed != "events" || cursor.Filter != filter {
			return Result{}, reason.New(reason.ReasonCursorInvalid, "cursor is invalid")
		}
	}
	// Establish the monotonic deadline before the initial snapshot. Every
	// storage operation receives this deadline, including the first read.
	deadline := time.Now().Add(timeout)
	watchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	result := Result{AuthorityID: st.AuthorityID(), Cursor: req.Cursor, NextCursor: req.Cursor, Resources: []ResourceState{}}
	position, initial, gap, err := snapshot(watchCtx, st, req.Resources, req.Cursor, filter, clock.Now())
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			result.TimedOut = true
			return result, nil
		}
		return result, err
	}
	result.Resources, result.Free = initial.States, initial.Free
	result.UnresolvedPredecessor = initial.Unresolved
	result.UnresolvedOperations = operationIDs(initial.Unresolved)
	result.NextCursor = positionCursor(st, filter, position)
	if !time.Now().Before(deadline) {
		result.TimedOut = true
		return result, nil
	}
	if gap {
		result.Gap, result.ResetCursor = true, result.NextCursor
		return result, nil
	}
	previousStates := stateMap(initial.States)
	if req.Until == "free" && initial.Free {
		return result, nil
	}
	canScanEvents := st.AuthorityID() != "" && !st.Empty()
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !time.Now().Before(deadline) {
			result.TimedOut = true
			return result, nil
		}
		if canScanEvents && (req.Cursor != "" || req.Until != "") {
			scan, scanErr := ledger.New(st).ScanEvents(watchCtx, result.NextCursor, filter)
			if scanErr != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				if !time.Now().Before(deadline) {
					result.NextCursor = positionCursor(st, filter, scanPosition(scan, result.NextCursor))
					result.TimedOut = true
					return result, nil
				}
				return result, scanErr
			}
			// A completed scan owns its terminal result even when the deadline
			// expires concurrently. Returning timeout first would advance past an
			// event or gap that the caller never received.
			if scan.Gap {
				result.Gap, result.ResetCursor, result.NextCursor = true, scan.NextCursor, scan.NextCursor
				return result, nil
			}
			result.NextCursor = scan.NextCursor
			if result.Cursor == "" && len(scan.Events) > 0 {
				result.Cursor = scan.NextCursor
			}
			if len(scan.Events) > 0 {
				if req.Until == "" {
					result.Event = &scan.Events[0]
					return result, nil
				}
				if req.Until == "change" {
					result.Changed = true
					return result, nil
				}
			}
			if !time.Now().Before(deadline) {
				result.TimedOut = true
				return result, nil
			}
		}
		if req.Until != "" {
			observation, stateErr := readState(watchCtx, st, req.Resources, clock.Now())
			if stateErr != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				if !time.Now().Before(deadline) {
					result.TimedOut = true
					return result, nil
				}
				return result, stateErr
			}
			if !time.Now().Before(deadline) {
				result.TimedOut = true
				return result, nil
			}
			result.Resources, result.Free = observation.States, observation.Free
			result.UnresolvedPredecessor = observation.Unresolved
			result.UnresolvedOperations = operationIDs(observation.Unresolved)
			current := stateMap(observation.States)
			if req.Until == "free" && observation.Free {
				return result, nil
			}
			if req.Until == "change" && statesChanged(previousStates, current) {
				result.Changed = true
				return result, nil
			}
			previousStates = current
		}
		wait := poll
		if req.Until != "" {
			if expiry, ok := nearestExpiry(result.Resources, clock.Now()); ok {
				untilExpiry := time.Until(expiry)
				if untilExpiry > 0 && untilExpiry < wait {
					wait = untilExpiry
				}
			}
		}
		remaining := time.Until(deadline)
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return result, ctx.Err()
		case <-watchCtx.Done():
			stopTimer(timer)
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			result.TimedOut = true
			return result, nil
		case <-timer.C:
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
func operationIDs(values []Predecessor) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.OperationID)
	}
	return result
}
func positionCursor(st *store.Store, filter string, position int64) string {
	if st.AuthorityID() == "" {
		return ""
	}
	return ledger.EncodeCursor(st.AuthorityID(), "events", filter, position)
}
func scanPosition(scan ledger.EventsScan, fallback string) int64 {
	if scan.InspectedSequence == "" {
		parsed, err := ledger.ParseCursor(fallback)
		if err != nil {
			return 0
		}
		var position int64
		if _, err := fmtSscanfDecimal(parsed.Sequence, &position); err != nil {
			return 0
		}
		return position
	}
	var position int64
	if _, err := fmtSscanfDecimal(scan.InspectedSequence, &position); err != nil {
		return 0
	}
	return position
}

func normalizeTimeout(timeout time.Duration) (time.Duration, error) {
	if timeout == 0 {
		return DefaultTimeout, nil
	}
	if timeout < 0 || timeout > MaxTimeout {
		return 0, reason.Invalid("timeout must be between 1s and 1h")
	}
	return timeout, nil
}

type observation struct {
	States     []ResourceState
	Free       bool
	Unresolved []Predecessor
	Now        time.Time
}

func snapshot(ctx context.Context, st *store.Store, resources []string, cursor, filter string, now time.Time) (int64, observation, bool, error) {
	position := int64(0)
	gap := false
	observed := freeObservation(resources, now)
	err := st.Read(ctx, func(tx *store.Tx) error {
		var last, pruned string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_event_seq'`).Scan(&last); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='pruned_through_seq'`).Scan(&pruned); err != nil {
			return err
		}
		var p int64
		if _, err := fmtSscanfDecimal(last, &position); err != nil {
			return reason.New(reason.ReasonStorageFailure, "invalid event watermark")
		}
		if _, err := fmtSscanfDecimal(pruned, &p); err != nil {
			return reason.New(reason.ReasonStorageFailure, "invalid event watermark")
		}
		if cursor != "" {
			c, err := ledger.ParseCursor(cursor)
			if err != nil {
				return err
			}
			if c.AuthorityID != st.AuthorityID() || c.Feed != "events" || c.Filter != filter {
				return reason.New(reason.ReasonCursorInvalid, "cursor is invalid")
			}
			if _, err := fmtSscanfDecimal(c.Sequence, &position); err != nil {
				return reason.New(reason.ReasonCursorInvalid, "cursor is invalid")
			}
			if position < p {
				position = p
				gap = true
			}
		}
		if len(resources) > 0 {
			var err error
			observed, err = readStateTx(ctx, tx, resources, now)
			return err
		}
		return nil
	})
	return position, observed, gap, err
}

// fmtSscanfDecimal intentionally accepts only the decimal-string cursor and
// watermark representation used by the ledger.
func fmtSscanfDecimal(value string, out *int64) (int, error) {
	if value == "" {
		return 0, errors.New("empty decimal")
	}
	var n int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid decimal")
		}
		n = n*10 + int64(r-'0')
	}
	*out = n
	return 1, nil
}

func freeObservation(resources []string, now time.Time) observation {
	result := observation{States: make([]ResourceState, 0, len(resources)), Free: true, Now: now}
	for _, resource := range resources {
		result.States = append(result.States, ResourceState{Resource: resource, State: "free"})
	}
	return result
}

func effectiveObservationNow(ctx context.Context, tx *store.Tx, now time.Time) (time.Time, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_observed_at'`).Scan(&raw); err != nil {
		return time.Time{}, reason.New(reason.ReasonStorageFailure, "read authority observation time")
	}
	var observed int64
	if _, err := fmtSscanfDecimal(raw, &observed); err != nil {
		return time.Time{}, reason.New(reason.ReasonStorageFailure, "invalid authority observation time")
	}
	if now.UnixMicro() < observed {
		now = time.UnixMicro(observed).UTC()
	}
	return now, nil
}

func readState(ctx context.Context, st *store.Store, resources []string, now time.Time) (observation, error) {
	result := freeObservation(resources, now)
	err := st.Read(ctx, func(tx *store.Tx) error {
		var err error
		result, err = readStateTx(ctx, tx, resources, now)
		return err
	})
	return result, err
}

func readStateTx(ctx context.Context, tx *store.Tx, resources []string, now time.Time) (observation, error) {
	effective, err := effectiveObservationNow(ctx, tx, now)
	if err != nil {
		return observation{}, err
	}
	result := observation{States: make([]ResourceState, 0, len(resources)), Free: true, Now: effective}
	for _, resource := range resources {
		var claimID string
		var expires int64
		err := tx.QueryRowContext(ctx, `SELECT c.claim_id,c.expires_at FROM claims c JOIN claim_resources cr ON cr.claim_id=c.claim_id WHERE cr.resource=?`, resource).Scan(&claimID, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			result.States = append(result.States, ResourceState{Resource: resource, State: "free"})
			continue
		}
		if err != nil {
			return result, err
		}
		at := time.UnixMicro(expires).UTC()
		state := "active"
		if !effective.Before(at) {
			state = "expired"
		}
		result.States = append(result.States, ResourceState{Resource: resource, State: state, ClaimID: claimID, ExpiresAt: at})
		if state == "active" {
			result.Free = false
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT o.claim_id,o.operation_id FROM operations o JOIN epoch_resources er ON er.claim_id=o.claim_id WHERE o.state='started' AND er.resource IN (`+placeholders(len(resources))+`) ORDER BY o.claim_id,o.operation_id`, stringArgs(resources)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Predecessor
		if err := rows.Scan(&p.ClaimID, &p.OperationID); err != nil {
			return result, err
		}
		p.Resources, err = epochResourcesForOperation(ctx, tx, p.ClaimID, p.OperationID)
		if err != nil {
			return result, err
		}
		result.Unresolved = append(result.Unresolved, p)
	}
	return result, rows.Err()
}

func epochResourcesForOperation(ctx context.Context, tx *store.Tx, claimID, operationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT er.resource FROM epoch_resources er JOIN operations o ON o.claim_id=er.claim_id WHERE er.claim_id=? AND o.operation_id=? ORDER BY er.resource`, claimID, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var resource string
		if err := rows.Scan(&resource); err != nil {
			return nil, err
		}
		result = append(result, resource)
	}
	return result, rows.Err()
}
func placeholders(n int) string {
	if n < 1 {
		return "?"
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}
func stringArgs(values []string) []any {
	result := make([]any, len(values))
	for i, v := range values {
		result[i] = v
	}
	return result
}
func stateMap(values []ResourceState) map[string]string {
	result := map[string]string{}
	for _, v := range values {
		// Claim identity is part of the observed state: a transfer or
		// reacquire can remain active while still being a meaningful change.
		result[v.Resource] = v.State + "\x00" + v.ClaimID + "\x00" + v.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}
func statesChanged(old, current map[string]string) bool {
	if len(old) != len(current) {
		return true
	}
	for key, value := range old {
		if current[key] != value {
			return true
		}
	}
	return false
}
func nearestExpiry(values []ResourceState, now time.Time) (time.Time, bool) {
	var nearest time.Time
	for _, value := range values {
		if value.State == "active" && value.ExpiresAt.After(now) && (nearest.IsZero() || value.ExpiresAt.Before(nearest)) {
			nearest = value.ExpiresAt
		}
	}
	return nearest, !nearest.IsZero()
}
