package watch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestWaitFiltersWithoutSkippingLaterMatchesAndReturnsScannedCursor(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "acquired", Resources: []string{"other"}, ClaimID: strings.Repeat("1", 32)}); err != nil {
			return err
		}
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "released", Resources: []string{"wanted"}, ClaimID: strings.Repeat("2", 32)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"wanted"}), 0)
	result, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"wanted"}, Timeout: time.Second, PollInterval: MinPoll})
	if err != nil {
		t.Fatal(err)
	}
	if result.Event == nil || result.Event.ClaimID != strings.Repeat("2", 32) {
		t.Fatalf("result=%+v", result)
	}
	parsed, err := ledger.ParseCursor(result.NextCursor)
	if err != nil || parsed.Sequence != "2" {
		t.Fatalf("cursor=%q err=%v", result.NextCursor, err)
	}
}

func TestWaitReportsRetainedPrefixGap(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "acquired", Resources: []string{"r"}, ClaimID: strings.Repeat("1", 32)}); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE meta SET value='1' WHERE key='pruned_through_seq'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"r"}), 0)
	result, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"r"}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Gap || result.ResetCursor == "" {
		t.Fatalf("result=%+v", result)
	}
	parsed, err := ledger.ParseCursor(result.ResetCursor)
	if err != nil || parsed.Sequence != "1" {
		t.Fatalf("reset=%q err=%v", result.ResetCursor, err)
	}
}

func TestWaitTimeoutCursorOnlyAdvancesThroughScannedRows(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "acquired", Resources: []string{"other"}, ClaimID: strings.Repeat("1", 32)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"wanted"}), 0)
	result, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"wanted"}, Timeout: 60 * time.Millisecond, PollInterval: MinPoll})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut {
		t.Fatalf("result=%+v", result)
	}
	parsed, err := ledger.ParseCursor(result.NextCursor)
	if err != nil || parsed.Sequence != "1" {
		t.Fatalf("cursor=%q err=%v", result.NextCursor, err)
	}
}

func TestWaitDoesNotReturnMatchAfterInitialDeadline(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "released", Resources: []string{"r"}, ClaimID: strings.Repeat("1", 32)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"r"}), 0)
	result, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"r"}, Timeout: time.Nanosecond, PollInterval: MinPoll})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.Event != nil {
		t.Fatalf("post-deadline event returned: %+v", result)
	}
}

func TestWaitUntilFreeDetectsLazyExpiryWithoutWrite(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	clock := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Second, PollInterval: MinPoll})
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"r"}, AgentID: "a", SessionID: "s", TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	before := countClaims(t, st)
	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, waitErr := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "free", Timeout: time.Second, PollInterval: MinPoll, Clock: clock})
		resultCh <- result
		errCh <- waitErr
	}()
	time.Sleep(20 * time.Millisecond)
	clock.Advance(2 * time.Second)
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		if !result.Free {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not wake after lazy expiry")
	}
	if after := countClaims(t, st); after != before {
		t.Fatalf("watch mutated claims: before=%d after=%d", before, after)
	}
}

func TestWaitUsesPersistedObservationTimeAfterClockRollback(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(base)
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Second, PollInterval: MinPoll})
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("3", 32), Token: strings.Repeat("b", 64), Resources: []string{"r"}, AgentID: "a", SessionID: "s", TTL: time.Second, RequestNotAfter: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	clock.SetWall(base.Add(-time.Minute))
	result, err := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "free", Timeout: 60 * time.Millisecond, PollInterval: MinPoll, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	if result.Free {
		t.Fatalf("clock rollback reported false freeness: %+v", result)
	}
	if !result.TimedOut {
		t.Fatalf("result=%+v", result)
	}
}

func TestWaitUntilChangeReturnsActiveReacquireEvent(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	oldClaim, newClaim := strings.Repeat("4", 32), strings.Repeat("5", 32)
	base := time.Now().UTC().Truncate(time.Microsecond)
	baseMicros := base.UnixMicro()
	if err := st.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO claims(claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, oldClaim, "hash", 1, "old", "s", "w", "local-coordination", 1, baseMicros, 1_000_000, baseMicros, baseMicros+2_000_000); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO claim_resources(resource,claim_id,position) VALUES(?,?,?)`, "r", oldClaim, 0); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	clock := testkit.NewClock(base.Add(500 * time.Millisecond))
	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, waitErr := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "change", Timeout: time.Second, PollInterval: MinPoll, Clock: clock})
		resultCh <- result
		errCh <- waitErr
	}()
	time.Sleep(20 * time.Millisecond)
	if err := st.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO claims(claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, newClaim, "hash", 1, "new", "s", "w", "local-coordination", 1, baseMicros, 1_000_000, baseMicros, baseMicros+3_000_000); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM claim_resources WHERE resource=?`, "r"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE claim_id=?`, oldClaim); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO claim_resources(resource,claim_id,position) VALUES(?,?,?)`, "r", newClaim, 0); err != nil {
			return err
		}
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "transferred", Resources: []string{"r"}, ClaimID: newClaim})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		if !result.Changed {
			t.Fatalf("active-to-active event was not reported: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not return on active-to-active transfer")
	}
}

func TestWaitBindsCursorToAuthorityFeedAndFilter(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	filter := ledger.ResourcesFilter([]string{"r"})
	cases := []string{
		ledger.EncodeCursor(strings.Repeat("f", 32), st.RestoreID(), "events", filter, 0),
		ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "history", filter, 0),
		ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"other"}), 0),
	}
	for _, cursor := range cases {
		if _, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"r"}, Timeout: time.Millisecond}); err == nil {
			t.Errorf("accepted mismatched cursor %q", cursor)
		}
	}
}

func TestWaitTimeoutContinuationDoesNotSkipLateEvent(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	filter := ledger.ResourcesFilter([]string{"r"})
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", filter, 0)
	writeErr := make(chan error, 1)
	go func() {
		time.Sleep(275 * time.Millisecond)
		writeErr <- st.Write(ctx, func(tx *store.Tx) error {
			_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "released", Resources: []string{"r"}, ClaimID: strings.Repeat("8", 32)})
			return err
		})
	}()
	first, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"r"}, Timeout: 300 * time.Millisecond, PollInterval: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if !first.TimedOut || first.Event != nil {
		t.Fatalf("first=%+v", first)
	}
	continued, err := Wait(ctx, st, Request{Cursor: first.NextCursor, Resources: []string{"r"}, Timeout: time.Second, PollInterval: MinPoll})
	if err != nil {
		t.Fatal(err)
	}
	if continued.Event == nil || continued.Event.ClaimID != strings.Repeat("8", 32) {
		t.Fatalf("late event was skipped: first=%+v continued=%+v", first, continued)
	}
}

func TestWaitUntilFreeKeepsWaitingThroughTransfer(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	clock := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Second, PollInterval: MinPoll})
	oldID, oldToken := strings.Repeat("9", 32), strings.Repeat("d", 64)
	grant, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Resources: []string{"r"}, AgentID: "old", SessionID: "old", TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, waitErr := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "free", Timeout: time.Second, PollInterval: MinPoll, Clock: clock})
		resultCh <- result
		errCh <- waitErr
	}()
	time.Sleep(20 * time.Millisecond)
	newID, newToken := strings.Repeat("a", 32), strings.Repeat("e", 64)
	successor, err := svc.Transfer(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Revision: grant.Revision}, lease.TransferRequest{OperationID: strings.Repeat("b", 32), SuccessorClaimID: newID, SuccessorToken: newToken, ToAgent: "new", ToSession: "new", ToWorkKey: "new", TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultCh:
		t.Fatalf("watch returned while successor was active: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Revision: successor.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("c", 32), Reason: "done", RequestNotAfter: clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		if !result.Free {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not return after successor release")
	}
}

func TestManyWaitersCancelWithoutLeakingOrBlockingWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"r"}), 0)
	const waiters = 24
	done := make(chan error, waiters)
	for range waiters {
		go func() {
			_, err := Wait(ctx, st, Request{Cursor: cursor, Resources: []string{"r"}, Timeout: time.Second, PollInterval: MinPoll})
			done <- err
		}()
	}
	time.Sleep(75 * time.Millisecond)
	writeCtx, stopWrite := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer stopWrite()
	if err := st.Write(writeCtx, func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "acquired", Resources: []string{"other"}, ClaimID: strings.Repeat("d", 32)})
		return err
	}); err != nil {
		t.Fatalf("waiters held transactions between polls: %v", err)
	}
	cancel()
	for range waiters {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("wait error=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("waiter leaked after cancellation")
		}
	}
}

func TestTimeoutDefaultsAndMaximum(t *testing.T) {
	if got, err := normalizeTimeout(0); err != nil || got != DefaultTimeout {
		t.Fatalf("default timeout=%v err=%v", got, err)
	}
	if got, err := normalizeTimeout(MaxTimeout); err != nil || got != MaxTimeout {
		t.Fatalf("maximum timeout=%v err=%v", got, err)
	}
	if _, err := normalizeTimeout(MaxTimeout + time.Nanosecond); err == nil {
		t.Fatal("accepted timeout above maximum")
	}
}

func TestEmptyFeedReturnsDurableBoundCursor(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	result, err := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "change", Timeout: 60 * time.Millisecond, PollInterval: MinPoll})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := ledger.ParseCursor(result.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || cursor.AuthorityID != st.AuthorityID() || cursor.Feed != "events" || cursor.Filter != ledger.ResourcesFilter([]string{"r"}) || cursor.Sequence != "0" {
		t.Fatalf("result=%+v cursor=%+v", result, cursor)
	}
}

func TestWaitRejectsResourcesWithoutUntilOrCursor(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := Wait(context.Background(), st, Request{Resources: []string{"r"}, Timeout: time.Millisecond}); err == nil {
		t.Fatal("watch accepted resources without until or cursor")
	}
}

func TestWaitUsesLaterPersistedObservationToClassifyExpiry(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(base)
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Second, PollInterval: MinPoll})
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("6", 32), Token: strings.Repeat("c", 64), Resources: []string{"r"}, AgentID: "a", SessionID: "s", TTL: time.Second, RequestNotAfter: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	if err := st.WriteAt(ctx, clock.Now(), func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: clock.Now(), Kind: "acquired", Resources: []string{"other"}, ClaimID: strings.Repeat("7", 32)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	clock.SetWall(base.Add(-time.Minute))
	result, err := Wait(ctx, st, Request{Resources: []string{"r"}, Until: "free", Timeout: time.Second, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Free || len(result.Resources) != 1 || result.Resources[0].State != "expired" {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidateResourcesUsesCanonicalRules(t *testing.T) {
	cases := []string{"\xff", strings.Repeat("a", 1025), "bad\x01resource"}
	for _, value := range cases {
		if err := ValidateResources([]string{value}); err == nil {
			t.Errorf("resource %q accepted", value)
		}
	}
}

func countClaims(t *testing.T, st *store.Store) int {
	t.Helper()
	count := 0
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM claims`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}
