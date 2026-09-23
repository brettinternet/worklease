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
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_, err := q.schedule(context.Background(), PriorityBackground, "excess", "", true, func(context.Context) (any, error) { return nil, nil })
	if d, ok := err.(ScheduleDiagnostic); !ok || d.Code != "overloaded" {
		t.Fatalf("overload: %v", err)
	}
	cancel()
	<-bgDone
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
