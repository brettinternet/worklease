package queue

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// RequestPriority orders provider work, not claim heartbeats (which use a separate path).
type RequestPriority int

const (
	PriorityAction RequestPriority = iota
	PriorityDetail
	PriorityVisible
	PriorityBackground
)

type ScheduleDiagnostic struct {
	Code    string
	RetryAt time.Time
}

func (e ScheduleDiagnostic) Error() string { return e.Code }

type scheduledJob struct {
	key, supersedes string
	priority        RequestPriority
	safe            bool
	ctx             context.Context
	cancel          context.CancelFunc
	run             func(context.Context) (any, error)
	done            chan struct{}
	result          any
	err             error
	waiters         int
	started         bool
}
type quotaQueue struct {
	mu                    sync.Mutex
	running               int
	limit                 int
	maxPending            int
	pending               []*scheduledJob
	active                map[*scheduledJob]bool
	inFlight              map[string]*scheduledJob
	retryAt, lastMutation time.Time
	now                   func() time.Time
	after                 func(time.Duration) <-chan time.Time
}

var quotaQueues sync.Map // quota identity -> *quotaQueue, process-local only
func quotaScheduler(identity string, concurrency int) *quotaQueue {
	value, _ := quotaQueues.LoadOrStore(identity, &quotaQueue{limit: concurrency, maxPending: 128, active: make(map[*scheduledJob]bool), inFlight: make(map[string]*scheduledJob), now: time.Now, after: time.After})
	return value.(*quotaQueue)
}
func (q *quotaQueue) remove(j *scheduledJob) {
	for i, candidate := range q.pending {
		if candidate == j {
			q.pending = append(q.pending[:i], q.pending[i+1:]...)
			break
		}
	}
	if j.key != "" && q.inFlight[j.key] == j {
		delete(q.inFlight, j.key)
	}
}
func (q *quotaQueue) start() {
	for q.running < q.limit && len(q.pending) > 0 {
		best := 0
		for i := 1; i < len(q.pending); i++ {
			if q.pending[i].priority < q.pending[best].priority {
				best = i
			}
		}
		j := q.pending[best]
		q.pending = append(q.pending[:best], q.pending[best+1:]...)
		j.started = true
		q.running++
		q.active[j] = true
		go func() {
			result, err := j.run(j.ctx)
			q.mu.Lock()
			j.result, j.err = result, err
			close(j.done)
			q.running--
			delete(q.active, j)
			q.remove(j)
			q.start()
			q.mu.Unlock()
			j.cancel()
		}()
	}
}

// schedule coalesces identical safe reads and drops a superseded queued read.
// Keys include credential generation and the complete request representation.
func (q *quotaQueue) schedule(ctx context.Context, priority RequestPriority, key, supersedes string, safe bool, run func(context.Context) (any, error)) (any, error) {
	q.mu.Lock()
	j := q.inFlight[key]
	if !safe || key == "" {
		j = nil
	}
	if j == nil {
		for _, old := range append([]*scheduledJob(nil), q.pending...) {
			if supersedes != "" && old.supersedes == supersedes {
				old.cancel()
				old.err = ScheduleDiagnostic{Code: "superseded"}
				close(old.done)
				q.remove(old)
			}
		}
		if supersedes != "" {
			for old := range q.active {
				if old.supersedes == supersedes && old.safe {
					old.cancel()
				}
			}
		}
		if len(q.pending) >= q.maxPending {
			// Reserve a pending slot for an authoritative action check.
			if priority == PriorityAction {
				for i := len(q.pending) - 1; i >= 0; i-- {
					old := q.pending[i]
					if old.priority == PriorityBackground && old.safe {
						old.cancel()
						old.err = ScheduleDiagnostic{Code: "superseded"}
						close(old.done)
						q.remove(old)
						break
					}
				}
			}
			if len(q.pending) >= q.maxPending {
				q.mu.Unlock()
				return nil, ScheduleDiagnostic{Code: "overloaded"}
			}
		}
		workCtx, cancel := context.WithCancel(context.Background())
		j = &scheduledJob{key: key, supersedes: supersedes, priority: priority, safe: safe, ctx: workCtx, cancel: cancel, run: run, done: make(chan struct{})}
		if safe && key != "" {
			q.inFlight[key] = j
		}
		q.pending = append(q.pending, j)
	}
	j.waiters++
	// Authoritative checks preempt cancellable background reads, never writes.
	if priority == PriorityAction {
		for active := range q.active {
			if active.priority == PriorityBackground && active.safe {
				active.cancel()
			}
		}
	}
	// Upgrade queued coalesced work if an action check joins it.
	if !j.started && priority < j.priority {
		j.priority = priority
	}
	q.start()
	q.mu.Unlock()
	select {
	case <-j.done:
		return j.result, j.err
	case <-ctx.Done():
		q.mu.Lock()
		j.waiters--
		if j.waiters == 0 {
			j.cancel()
			if !j.started {
				j.err = ctx.Err()
				close(j.done)
				q.remove(j)
			}
		}
		q.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (q *quotaQueue) retryDeadline() time.Time { q.mu.Lock(); defer q.mu.Unlock(); return q.retryAt }

// waitQuota reports the retry deadline rather than making action checks wait behind
// background backoff. Jitter avoids synchronizing independent client processes.
func (q *quotaQueue) waitQuota(ctx context.Context) error {
	q.mu.Lock()
	until := q.retryAt
	now := q.now()
	q.mu.Unlock()
	if until.After(now) {
		return ScheduleDiagnostic{Code: "rate-limited", RetryAt: until}
	}
	return ctx.Err()
}
func (q *quotaQueue) limitUntil(until time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if until.After(q.retryAt) {
		q.retryAt = until.Add(time.Duration(rand.Int63n(int64(250 * time.Millisecond))))
	}
}

// mutationSlot spaces future GitHub writes, without retrying an uncertain write.
func (q *quotaQueue) mutationSlot(ctx context.Context) error {
	q.mu.Lock()
	now := q.now()
	next := q.lastMutation.Add(time.Second)
	if next.Before(now) {
		next = now
	}
	q.lastMutation = next // reserve before waiting so concurrent writes cannot share a slot
	delay := next.Sub(now)
	q.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.after(delay):
		}
	}
	if err := q.waitQuota(ctx); err != nil {
		return err
	}
	return nil
}
