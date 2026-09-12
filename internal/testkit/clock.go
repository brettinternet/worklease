package testkit

import (
	"sync"
	"time"
)

// Clock is a race-safe controllable wall and monotonic clock.
type Clock struct {
	mu        sync.RWMutex
	wall      time.Time
	monotonic time.Duration
}

// NewClock returns a clock at the supplied wall time and monotonic zero.
func NewClock(wall time.Time) *Clock { return &Clock{wall: wall} }

// Now returns the current wall time.
func (c *Clock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.wall
}

// Monotonic returns elapsed monotonic time.
func (c *Clock) Monotonic() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.monotonic
}

// Advance moves both clocks forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
	c.monotonic += d
}

// SetWall changes wall time without changing monotonic time.
func (c *Clock) SetWall(wall time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = wall
}
