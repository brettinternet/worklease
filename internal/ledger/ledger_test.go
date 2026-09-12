package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

type ids struct{ n int }

func (i *ids) Generate() string { i.n++; return strings.Repeat("0", 31) + string(rune('0'+i.n)) }
func fixture(t *testing.T) (*Service, *lease.Service, *store.Store, *testkit.Clock) {
	t.Helper()
	clock := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	st, e := store.Open(context.Background(), t.TempDir(), store.Options{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	return New(st), lease.New(st, clock, &ids{}, lease.Defaults{TTL: time.Minute}), st, clock
}
func acquire(t *testing.T, svc *lease.Service, st *store.Store, claim, token string, resources []string) *lease.Grant {
	t.Helper()
	g, e := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: resources, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	return &g
}

func TestInspectPublicRedactionAuthenticatedFullStartedAndAmbiguousEpochs(t *testing.T) {
	ledgerSvc, leaseSvc, st, clock := fixture(t)
	ctx := context.Background()
	claim, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	g := acquire(t, leaseSvc, st, claim, token, []string{"r"})
	op := strings.Repeat("2", 32)
	started, e := leaseSvc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}, lease.OperationIntent{OperationID: op, Kind: "exec", Request: map[string]any{"argv": []string{"secret"}}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	public, e := ledgerSvc.Inspect(ctx, InspectRequest{OperationID: op, Resource: "r"})
	if e != nil {
		t.Fatal(e)
	}
	if public.State != "started" || public.RequestSHA256 != started.RequestHash || public.Receipt != nil || public.Evidence != nil {
		t.Fatalf("public=%+v", public)
	}
	if _, e = ledgerSvc.Inspect(ctx, InspectRequest{OperationID: op, ClaimID: claim, Token: strings.Repeat("b", 64), Full: true}); reason.As(e) == nil || reason.As(e).Reason != reason.ReasonInvalidToken {
		t.Fatalf("invalid token err=%v", e)
	}
	full, e := ledgerSvc.Inspect(ctx, InspectRequest{OperationID: op, ClaimID: claim, Token: token, Full: true})
	if e != nil || full.ExpectedRevision != g.Revision {
		t.Fatalf("full=%+v err=%v", full, e)
	}
	clock.Advance(2 * time.Minute)
	newClaim, newToken := strings.Repeat("3", 32), strings.Repeat("b", 64)
	newGrant := acquire(t, leaseSvc, st, newClaim, newToken, []string{"r"})
	if _, e = leaseSvc.Heartbeat(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: newClaim, Token: newToken, Revision: newGrant.Revision}, lease.Renew{OperationID: op, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if _, e = ledgerSvc.Inspect(ctx, InspectRequest{OperationID: op, Resource: "r"}); reason.As(e) == nil || reason.As(e).Reason != reason.ReasonOperationAmbiguous {
		t.Fatalf("ambiguous err=%v", e)
	}
	if _, e = ledgerSvc.Inspect(ctx, InspectRequest{OperationID: op, ClaimID: claim}); e != nil {
		t.Fatal(e)
	}
}

func TestEventsCursorBindingPaginationWatermarkAndGap(t *testing.T) {
	ledgerSvc, leaseSvc, st, _ := fixture(t)
	ctx := context.Background()
	token := strings.Repeat("a", 64)
	acquire(t, leaseSvc, st, strings.Repeat("1", 32), token, []string{"r"})
	first, e := ledgerSvc.Events(ctx, "", 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Events) != 1 || first.NextCursor == "" {
		t.Fatalf("first=%+v", first)
	}
	if e := ValidateCursor(first.NextCursor, "history", "r"); reason.As(e) == nil || reason.As(e).Reason != reason.ReasonCursorInvalid {
		t.Fatalf("feed bind err=%v", e)
	}
	duplicate := base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"version":1,"authorityId":"` + st.AuthorityID() + `","feed":"events","filter":"","sequence":"1"}`))
	if e := ValidateCursor(duplicate, "events", ""); reason.As(e) == nil || reason.As(e).Reason != reason.ReasonCursorInvalid {
		t.Fatalf("duplicate cursor err=%v", e)
	}
	if _, e := ledgerSvc.Events(ctx, first.NextCursor, 1); e != nil {
		t.Fatal(e)
	}
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE meta SET value='1' WHERE key='pruned_through_seq'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	zero := encodeCursor(st.AuthorityID(), "events", "", 0)
	gap, e := ledgerSvc.Events(ctx, zero, 1)
	if e != nil {
		t.Fatal(e)
	}
	if !gap.Gap || len(gap.Events) != 0 {
		t.Fatalf("gap=%+v", gap)
	}
	emptyHome := t.TempDir()
	bad := "not-a-cursor"
	if e := ValidateCursor(bad, "events", ""); e == nil {
		t.Fatal("malformed cursor accepted")
	}
	entries, e := os.ReadDir(emptyHome)
	if e != nil || len(entries) != 0 {
		t.Fatalf("cursor validation touched storage: entries=%v err=%v", entries, e)
	}
}

func TestHistoryExactResourceCoverageAndNeverReturnsPrivatePayloads(t *testing.T) {
	ledgerSvc, leaseSvc, st, _ := fixture(t)
	ctx := context.Background()
	claim, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	g := acquire(t, leaseSvc, st, claim, token, []string{"a", "b"})
	op := strings.Repeat("2", 32)
	started, e := leaseSvc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}, lease.OperationIntent{OperationID: op, Kind: "exec", Request: map[string]any{"argv": []string{"private"}}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = leaseSvc.CompleteOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: started.Revision}, op, map[string]any{"output": "private", "exitStatus": 0})
	if e != nil {
		t.Fatal(e)
	}
	page, e := ledgerSvc.History(ctx, "b", "", 10, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Epochs) != 1 || len(page.Epochs[0].Resources) != 2 || len(page.Epochs[0].Operations) != 2 {
		t.Fatalf("page=%+v", page)
	}
	encoded, _ := json.Marshal(page)
	text := string(encoded)
	if strings.Contains(text, "private") || strings.Contains(text, "receipt") || strings.Contains(text, "evidence") || strings.Contains(text, "checkpoint") {
		t.Fatalf("history leaked private payload: %s", text)
	}
	if _, e = ledgerSvc.History(ctx, "missing", "", 10, false); e != nil {
		t.Fatal(e)
	}
}
