package cli

import (
	"context"
	"sync"
)

// queueRefreshRunner serializes full TUI refreshes. Triggers during a pass
// coalesce into one follow-up pass without retaining a waiter per watch event.
// Manual refreshes complete after their scheduled pass even if more events arrive.
type queueRefreshRunner struct {
	mu            sync.Mutex
	workers       *sync.WaitGroup
	run           func() error
	report        func(error)
	closed        bool
	running       bool
	pending       bool
	notify        bool
	pendingNotify bool
	current       []chan error
	next          []chan error
}

func (r *queueRefreshRunner) start() <-chan error {
	done := make(chan error, 1)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		done <- context.Canceled
		close(done)
		return done
	}
	if r.running {
		r.pending = true
		r.next = append(r.next, done)
	} else {
		r.current = append(r.current, done)
		r.launchLocked()
	}
	r.mu.Unlock()
	return done
}

func (r *queueRefreshRunner) trigger() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.running {
		r.pending = true
		r.pendingNotify = true
	} else {
		r.notify = true
		r.launchLocked()
	}
}

func (r *queueRefreshRunner) launchLocked() {
	r.running = true
	r.workers.Add(1)
	go r.loop()
}

func (r *queueRefreshRunner) stop() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
}

func (r *queueRefreshRunner) loop() {
	defer r.workers.Done()
	for {
		err := r.run()
		r.mu.Lock()
		waiters := r.current
		notify := r.notify
		closed := r.closed
		pending := r.pending && !closed
		if pending {
			r.current, r.next = r.next, nil
			r.notify, r.pendingNotify = r.pendingNotify, false
			r.pending = false
		} else {
			r.running = false
			r.current = nil
			r.notify = false
		}
		remaining := r.next
		if !pending {
			r.next = nil
		}
		r.mu.Unlock()
		for _, done := range waiters {
			done <- err
			close(done)
		}
		if closed {
			for _, done := range remaining {
				done <- context.Canceled
				close(done)
			}
		}
		if pending {
			continue
		}
		if notify && err != nil && !closed {
			r.report(err)
		}
		return
	}
}
