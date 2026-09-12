package lease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func bundleRequest(st *store.Store, resources []string, digit, tokenByte string, deadline time.Time) AcquireRequest {
	return AcquireRequest{
		AuthorityID:     st.AuthorityID(),
		ClaimID:         strings.Repeat(digit, 32),
		Token:           strings.Repeat(tokenByte, 64),
		Resources:       resources,
		AgentID:         "agent-" + digit,
		SessionID:       "session-" + digit,
		TTL:             2 * time.Second,
		RequestNotAfter: deadline,
	}
}

func TestBundleValidationAcceptsBoundsAndRejectsInvalidShapesBeforeWrites(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	valid := make([]string, 32)
	for i := range valid {
		valid[i] = fmt.Sprintf("resource-%02d", i)
	}
	grant, err := svc.Acquire(context.Background(), bundleRequest(st, valid, "1", "a", deadline))
	if err != nil || len(grant.Resources) != 32 {
		t.Fatalf("32-resource acquire=%+v err=%v", grant, err)
	}

	invalid := [][]string{nil, {}, {""}, {"duplicate", "duplicate"}, {" leading"}, {"line\nbreak"}, append(append([]string(nil), valid...), "resource-33")}
	for i, resources := range invalid {
		request := bundleRequest(st, resources, fmt.Sprintf("%x", i+2), fmt.Sprintf("%x", i+2), deadline)
		_, err := svc.Acquire(context.Background(), request)
		if e := reason.As(err); e == nil || e.Reason != reason.ReasonInvalidResource {
			t.Fatalf("resources=%q reason=%v", resources, err)
		}
	}
	status, err := svc.List(context.Background(), "")
	if err != nil || len(status) != 1 || status[0].ClaimID != grant.ClaimID {
		t.Fatalf("invalid request changed state: %+v err=%v", status, err)
	}
}

func TestBundleAcquireContentionUsesCallerOrderAndRollsBack(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	if _, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"busy-one"}, "1", "a", deadline)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"busy-two"}, "2", "b", deadline)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"free", "busy-two", "busy-one"}, "3", "c", deadline))
	failure := reason.As(err)
	if failure == nil || failure.Reason != reason.ReasonAlreadyClaimed || failure.Details["resource"] != "busy-two" {
		t.Fatalf("contention=%v", err)
	}
	status, err := svc.Status(context.Background(), Selector{Resources: []string{"free", "busy-two", "busy-one"}})
	if err != nil || len(status.Resources) != 3 || status.Resources[0].State != "free" || status.Resources[1].Claim == nil || status.Resources[1].Claim.ClaimID != strings.Repeat("2", 32) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestBundleAcquireRollsBackAfterPartialMemberFailure(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	beforeClaimResourceInsert = func(index int, _ string) error {
		if index == 1 {
			return errors.New("injected member failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeClaimResourceInsert = nil })
	_, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"one", "two", "three"}, "1", "a", deadline))
	if err == nil {
		t.Fatal("expected injected failure")
	}
	beforeClaimResourceInsert = nil
	status, err := svc.Status(context.Background(), Selector{Resources: []string{"one", "two", "three"}})
	if err != nil || len(status.Claims) != 0 {
		t.Fatalf("partial claim remained: %+v err=%v", status, err)
	}
	for _, resource := range status.Resources {
		if resource.State != "free" {
			t.Fatalf("resource not rolled back: %+v", resource)
		}
	}
	var epochs int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM epochs`).Scan(&epochs)
	}); err != nil || epochs != 0 {
		t.Fatalf("epochs=%d err=%v", epochs, err)
	}
}

func TestBundleExpiredReplacementFinalizesClaimsOnceAndRemovesWholeProjection(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	first, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"a", "orphaned"}, "1", "a", deadline))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"b"}, "2", "b", deadline))
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	next, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"a", "b"}, "3", "c", deadline))
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Recovery) != 2 || next.Recovery[0].Resource != "a" || next.Recovery[1].Resource != "b" {
		t.Fatalf("recovery=%+v", next.Recovery)
	}
	status, err := svc.Status(context.Background(), Selector{Resources: []string{"a", "orphaned", "b"}})
	if err != nil || status.Resources[1].State != "free" || status.Resources[0].Claim == nil || status.Resources[0].Claim.ClaimID != next.ClaimID {
		t.Fatalf("replacement status=%+v err=%v", status, err)
	}
	var firstEnd, secondEnd int64
	var firstEvents, secondEvents int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT ended_at FROM epochs WHERE claim_id=?`, first.ClaimID).Scan(&firstEnd); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT ended_at FROM epochs WHERE claim_id=?`, second.ClaimID).Scan(&secondEnd); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE claim_id=? AND kind='expired-replaced'`, first.ClaimID).Scan(&firstEvents); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE claim_id=? AND kind='expired-replaced'`, second.ClaimID).Scan(&secondEvents)
	}); err != nil {
		t.Fatal(err)
	}
	if firstEnd != first.ExpiresAt.UnixMicro() || secondEnd != second.ExpiresAt.UnixMicro() || firstEvents != 1 || secondEvents != 1 {
		t.Fatalf("ends=(%d,%d) events=(%d,%d)", firstEnd, secondEnd, firstEvents, secondEvents)
	}
}

func TestBundlePendingPredecessorRequiresCompleteResourceCoverage(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	old := bundleRequest(st, []string{"one", "two"}, "1", "a", deadline)
	grant, err := svc.Acquire(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	started, err := svc.BeginOperation(context.Background(), credentials(st, grant.ClaimID, old.Token, grant.Revision), OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", RequestNotAfter: deadline, TTL: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	_, err = svc.Acquire(context.Background(), bundleRequest(st, []string{"two"}, "3", "b", deadline))
	failure := reason.As(err)
	if failure == nil || failure.Reason != reason.ReasonOperationInProgress || fmt.Sprint(failure.Details["requiredResources"]) != "[two one]" {
		t.Fatalf("partial recovery=%v", err)
	}
	covering, err := svc.Acquire(context.Background(), bundleRequest(st, []string{"two", "one"}, "4", "c", deadline))
	if err != nil {
		t.Fatal(err)
	}
	if len(covering.UnknownOperations) != 1 || covering.UnknownOperations[0] != started.OperationID {
		t.Fatalf("unknown=%v", covering.UnknownOperations)
	}
}

func TestBundleLifecycleAndStatusOperateOnWholeClaim(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	request := bundleRequest(st, []string{"first", "second", "third"}, "1", "a", deadline)
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := svc.Checkpoint(context.Background(), credentials(st, grant.ClaimID, request.Token, grant.Revision), CheckpointRequest{OperationID: strings.Repeat("2", 32), Data: []byte(`{"phase":"bundle"}`), RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	successorToken := strings.Repeat("b", 64)
	successor, err := svc.Transfer(context.Background(), credentials(st, grant.ClaimID, request.Token, checkpoint.Revision), TransferRequest{OperationID: strings.Repeat("3", 32), SuccessorClaimID: strings.Repeat("4", 32), SuccessorToken: successorToken, ToAgent: "next", ToSession: "next-session", ToWorkKey: "next-work", RequestNotAfter: deadline})
	if err != nil || len(successor.Resources) != 3 {
		t.Fatalf("successor=%+v err=%v", successor, err)
	}
	status, err := svc.Status(context.Background(), Selector{Resources: []string{"third", "free", "first"}})
	if err != nil || len(status.Resources) != 3 || status.Resources[0].Claim == nil || status.Resources[0].Claim.ClaimID != successor.ClaimID || status.Resources[1].State != "free" || status.Resources[2].Claim == nil || status.Resources[2].Claim.ClaimID != successor.ClaimID || len(status.Claims) != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := svc.Release(context.Background(), credentials(st, successor.ClaimID, successorToken, successor.Revision), ReleaseRequest{OperationID: strings.Repeat("5", 32), RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	status, err = svc.Status(context.Background(), Selector{Resources: request.Resources})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range status.Resources {
		if resource.State != "free" {
			t.Fatalf("release left member claimed: %+v", resource)
		}
	}
}

func TestOverlappingBundlesAcrossProcessesHaveOneWinner(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		home := t.TempDir()
		bootstrap, err := store.Open(context.Background(), home, store.Options{})
		if err != nil {
			t.Fatal(err)
		}
		authority := bootstrap.AuthorityID()
		if err := bootstrap.Close(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
		commands := []*exec.Cmd{
			exec.Command(os.Args[0], "-test.run=^TestBundleAcquireProcessHelper$", "--", home, authority, strings.Repeat("1", 32), strings.Repeat("a", 64), deadline, "a,shared"),
			exec.Command(os.Args[0], "-test.run=^TestBundleAcquireProcessHelper$", "--", home, authority, strings.Repeat("2", 32), strings.Repeat("b", 64), deadline, "shared,b"),
		}
		for _, command := range commands {
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
		}
		wins := 0
		for _, command := range commands {
			if err := command.Wait(); err == nil {
				wins++
			} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
				t.Fatalf("unexpected helper result: %v", err)
			}
		}
		if wins != 1 {
			t.Fatalf("attempt %d wins=%d", attempt, wins)
		}
	}
}

func TestBundleAcquireProcessHelper(t *testing.T) {
	if len(os.Args) < 8 || os.Args[len(os.Args)-7] != "--" {
		return
	}
	args := os.Args[len(os.Args)-6:]
	deadline, err := time.Parse(time.RFC3339Nano, args[4])
	if err != nil {
		os.Exit(64)
	}
	st, err := store.Open(context.Background(), args[0], store.Options{})
	if err != nil {
		os.Exit(75)
	}
	defer st.Close()
	_, err = New(st, nil, nil, Defaults{}).Acquire(context.Background(), AcquireRequest{AuthorityID: args[1], ClaimID: args[2], Token: args[3], Resources: strings.Split(args[5], ","), AgentID: args[2], SessionID: args[2], TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		os.Exit(2)
	}
}
