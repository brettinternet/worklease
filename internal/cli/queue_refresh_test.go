package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestQueueRefreshRunnerCoalescesWatchEventsWithoutDelayingManualCompletion(t *testing.T) {
	t.Parallel()
	started := make(chan int, 3)
	finish := make(chan error)
	reported := make(chan error, 1)
	var workers sync.WaitGroup
	calls := 0
	runner := &queueRefreshRunner{workers: &workers, run: func() error {
		calls++ // only one run may execute at a time
		started <- calls
		return <-finish
	}, report: func(err error) { reported <- err }}
	runner.trigger()
	if got := <-started; got != 1 {
		t.Fatalf("first pass = %d", got)
	}
	manual := runner.start()
	for range 100 {
		runner.trigger()
	}
	runner.mu.Lock()
	pendingWaiters := len(runner.next)
	runner.mu.Unlock()
	if pendingWaiters != 1 {
		t.Fatalf("watch events retained %d waiters, want only manual request", pendingWaiters)
	}
	finish <- errors.New("observation-invalidated")
	select {
	case got := <-started:
		if got != 2 {
			t.Fatalf("coalesced pass = %d", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending refresh did not run")
	}
	select {
	case err := <-reported:
		t.Fatalf("notified superseded failure: %v", err)
	default:
	}
	for range 100 {
		runner.trigger()
	}
	finish <- nil
	select {
	case err := <-manual:
		if err != nil {
			t.Fatalf("manual refresh failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manual refresh waited for later watch passes")
	}
	select {
	case got := <-started:
		if got != 3 {
			t.Fatalf("later watch pass = %d", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("latest watch event was lost")
	}
	finish <- nil
	runner.stop()
	workers.Wait()
	select {
	case err := <-reported:
		t.Fatalf("unexpected failure notification: %v", err)
	default:
	}
}

func TestQueueRefreshRunnerDoesNotNotifyManualFailureAfterWatchPass(t *testing.T) {
	t.Parallel()
	failure := errors.New("provider-failed")
	reported := make(chan error, 1)
	var workers sync.WaitGroup
	calls := 0
	runner := &queueRefreshRunner{workers: &workers, run: func() error {
		calls++
		if calls == 1 {
			return nil
		}
		return failure
	}, report: func(err error) { reported <- err }}
	runner.trigger()
	workers.Wait()
	if got := <-runner.start(); !errors.Is(got, failure) {
		t.Fatalf("manual failure = %v", got)
	}
	runner.stop()
	workers.Wait()
	select {
	case got := <-reported:
		t.Fatalf("manual failure was also sent as watch notification: %v", got)
	default:
	}
}

func TestQueueRefreshRunnerReportsUnsupersededFailureAndStops(t *testing.T) {
	t.Parallel()
	failure := errors.New("provider-failed")
	reported := make(chan error, 1)
	var workers sync.WaitGroup
	runner := &queueRefreshRunner{workers: &workers, run: func() error { return failure }, report: func(err error) { reported <- err }}
	runner.trigger()
	select {
	case got := <-reported:
		if !errors.Is(got, failure) {
			t.Fatalf("notified failure = %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failure was not reported")
	}
	runner.stop()
	if got := <-runner.start(); !errors.Is(got, context.Canceled) {
		t.Fatalf("closed runner returned %v", got)
	}
	workers.Wait()
}
