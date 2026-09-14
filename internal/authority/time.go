// Package authority contains the client boundary shared by command consumers.
package authority

import (
	"fmt"
	"sync"
	"time"
)

// AuthorityClock keeps conservative bounds for one authority-time sample.
// S and R are the local send/receive times and A is the authority timestamp.
// At local time N, lower=A+(N-R), upper=A+(N-S).
type AuthorityClock struct {
	mu                        sync.RWMutex
	now                       func() time.Time
	authority, sent, received time.Time
	valid                     bool
}

func NewAuthorityClock(now func() time.Time) *AuthorityClock {
	if now == nil {
		now = time.Now
	}
	return &AuthorityClock{now: now}
}
func (c *AuthorityClock) Sample(authority, sent, received time.Time) error {
	if authority.IsZero() || received.Before(sent) {
		return fmt.Errorf("invalid authority-time sample")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && authority.Before(c.authority) {
		return fmt.Errorf("authority time regressed")
	}
	c.authority = authority.UTC()
	c.sent = sent
	c.received = received
	c.valid = true
	return nil
}
func (c *AuthorityClock) ResampleNeeded() bool { c.mu.RLock(); defer c.mu.RUnlock(); return !c.valid }
func (c *AuthorityClock) Invalidate()          { c.mu.Lock(); c.valid = false; c.mu.Unlock() }
func (c *AuthorityClock) MarkRestartOrResume() { c.Invalidate() }
func (c *AuthorityClock) bounds() (time.Time, time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.valid {
		return time.Time{}, time.Time{}, fmt.Errorf("authority time is unsampled")
	}
	n := c.now()
	return c.authority.Add(n.Sub(c.received)), c.authority.Add(n.Sub(c.sent)), nil
}
func (c *AuthorityClock) LowerBound() (time.Time, error) { l, _, e := c.bounds(); return l, e }
func (c *AuthorityClock) UpperBound() (time.Time, error) { _, u, e := c.bounds(); return u, e }
func (c *AuthorityClock) RenewalAt(start time.Time, ttl time.Duration) time.Time {
	return start.UTC().Add(ttl / 2)
}
func (c *AuthorityClock) StopNewAt(start time.Time, ttl time.Duration) time.Time {
	return start.UTC().Add(ttl * 3 / 4)
}
func (c *AuthorityClock) RequestNotAfter() (time.Time, error) {
	l, e := c.LowerBound()
	if e != nil {
		return time.Time{}, e
	}
	return l.Add(24 * time.Hour), nil
}

// DispatchAllowed reports whether a confirmed original start response arrived
// before the conservative authority expiry. It is never used for replayed starts.
func (c *AuthorityClock) DispatchAllowed(start time.Time, ttl time.Duration) bool {
	u, e := c.UpperBound()
	return e == nil && u.Before(start.UTC().Add(ttl))
}
func (c *AuthorityClock) ShouldRenew(start time.Time, ttl time.Duration) bool {
	u, e := c.UpperBound()
	return e == nil && !u.Before(start.UTC().Add(ttl/2))
}
func (c *AuthorityClock) CanStart(start time.Time, ttl time.Duration) bool {
	u, e := c.UpperBound()
	return e == nil && u.Before(start.UTC().Add(ttl*3/4))
}
