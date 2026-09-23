package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testQueue(limit int) *quotaQueue {
	return &quotaQueue{limit: limit, maxPending: 2, inFlight: map[string]*scheduledJob{}, active: map[*scheduledJob]bool{}, now: time.Now, after: time.After}
}
func TestSchedulePriorityCancellationAndOverload(t *testing.T) {
	q := testQueue(1)
	q.maxPending = 1
	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = q.schedule(context.Background(), PriorityDetail, "hold", "", true, func(context.Context) (any, error) { close(started); <-hold; return nil, nil })
	}()
	<-started
	order := make(chan string, 2)
	bgCtx, cancel := context.WithCancel(context.Background())
	bgDone := make(chan error, 1)
	go func() {
		_, err := q.schedule(bgCtx, PriorityBackground, "bg", "", true, func(context.Context) (any, error) { order <- "background"; return nil, nil })
		bgDone <- err
	}()
	// Wait for deterministic queued state before enqueueing action.
	for {
		q.mu.Lock()
		n := len(q.pending)
		q.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	actionDone := make(chan struct{})
	go func() {
		_, _ = q.schedule(context.Background(), PriorityAction, "action", "", true, func(context.Context) (any, error) { order <- "action"; return nil, nil })
		close(actionDone)
	}()
	for {
		q.mu.Lock()
		n := len(q.pending)
		q.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_, err := q.schedule(context.Background(), PriorityBackground, "excess", "", true, func(context.Context) (any, error) { return nil, nil })
	if d, ok := err.(ScheduleDiagnostic); !ok || d.Code != "overloaded" {
		t.Fatalf("overload: %v", err)
	}
	if d, ok := (<-bgDone).(ScheduleDiagnostic); !ok || d.Code != "superseded" {
		t.Fatalf("action did not evict background read: %v", d)
	}
	cancel()
	close(hold)
	<-actionDone
	if got := <-order; got != "action" {
		t.Fatalf("priority inversion: %s", got)
	}
}
func TestScheduleCoalesceAndSupersede(t *testing.T) {
	q := testQueue(1)
	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = q.schedule(context.Background(), PriorityDetail, "busy", "", true, func(context.Context) (any, error) { close(started); <-hold; return nil, nil })
	}()
	<-started
	var calls atomic.Int32
	oldDone := make(chan error, 1)
	go func() {
		_, err := q.schedule(context.Background(), PriorityBackground, "old", "page", true, func(context.Context) (any, error) { calls.Add(1); return nil, nil })
		oldDone <- err
	}()
	for {
		q.mu.Lock()
		n := len(q.pending)
		q.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	newDone := make(chan error, 2)
	run := func(context.Context) (any, error) { calls.Add(1); return "ok", nil }
	go func() {
		_, err := q.schedule(context.Background(), PriorityVisible, "new", "page", true, run)
		newDone <- err
	}()
	for {
		q.mu.Lock()
		n := len(q.pending)
		q.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	go func() {
		_, err := q.schedule(context.Background(), PriorityVisible, "new", "page", true, run)
		newDone <- err
	}()
	for {
		q.mu.Lock()
		joined := q.inFlight["new"] != nil && q.inFlight["new"].waiters == 2
		q.mu.Unlock()
		if joined {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if d, ok := (<-oldDone).(ScheduleDiagnostic); !ok || d.Code != "superseded" {
		t.Fatalf("supersession: %v", d)
	}
	close(hold)
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("coalesced calls=%d", calls.Load())
	}
}
func TestScheduleSupersedesRunningRead(t *testing.T) {
	q := testQueue(1)
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		_, _ = q.schedule(context.Background(), PriorityBackground, "old", "page", true, func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		})
	}()
	<-started
	result := make(chan error, 1)
	go func() {
		_, err := q.schedule(context.Background(), PriorityVisible, "new", "page", true, func(context.Context) (any, error) { return nil, nil })
		result <- err
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("running read not cancelled")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestCanceledLastWaiterDoesNotCoalesceNewRead(t *testing.T) {
	q := testQueue(1)
	started := make(chan struct{})
	canceled := make(chan struct{})
	releaseRunner := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := q.schedule(firstCtx, PriorityVisible, "same", "", true, func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-releaseRunner
			return nil, ctx.Err()
		})
		firstDone <- err
	}()
	<-started
	cancelFirst()
	<-canceled
	if err := <-firstDone; err == nil {
		t.Fatal("first waiter was not canceled")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := q.schedule(context.Background(), PriorityVisible, "same", "", true, func(context.Context) (any, error) { return "fresh", nil })
		secondDone <- err
	}()
	deadline := time.After(time.Second)
	for {
		q.mu.Lock()
		freshJob := q.inFlight["same"] != nil && q.inFlight["same"].run != nil && q.inFlight["same"].ctx.Err() == nil
		q.mu.Unlock()
		if freshJob {
			break
		}
		select {
		case <-deadline:
			t.Fatal("fresh request did not replace canceled coalescing entry")
		case <-time.After(time.Millisecond):
		}
	}
	close(releaseRunner)
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh request did not run after canceled job released slot")
	}
}

func TestScheduleFakeClockRateAndMutationSpacing(t *testing.T) {
	q := testQueue(1)
	clock := time.Unix(1000, 0)
	tick := make(chan time.Time, 2)
	q.now = func() time.Time { return clock }
	q.after = func(time.Duration) <-chan time.Time { return tick }
	q.limitUntil(clock.Add(3 * time.Second))
	if d, ok := q.waitQuota(context.Background()).(ScheduleDiagnostic); !ok || d.Code != "rate-limited" || !d.RetryAt.After(clock) {
		t.Fatalf("retry: %v", d)
	}
	q.mu.Lock()
	clock = clock.Add(4 * time.Second)
	q.mu.Unlock()
	if err := q.mutationSlot(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- q.mutationSlot(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("mutation spacing skipped: %v", err)
	case <-time.After(time.Millisecond):
	}
	q.mu.Lock()
	clock = clock.Add(time.Second)
	q.mu.Unlock()
	tick <- clock
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := q.schedule(context.Background(), PriorityAction, "", "", false, func(context.Context) (any, error) { return nil, errors.New("uncertain") }); err == nil {
		t.Fatal("uncertain write was retried")
	}
}
