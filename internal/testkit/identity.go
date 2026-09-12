package testkit

import (
	"fmt"
	"sync"
)

// Generator returns deterministic, race-safe test identifiers and tokens.
type Generator struct {
	mu   sync.Mutex
	next uint64
}

// NewGenerator returns a generator whose first value is derived from seed+1.
func NewGenerator(seed uint64) *Generator { return &Generator{next: seed} }

// ID returns a unique 32-character lowercase hexadecimal identifier.
func (g *Generator) ID() string {
	return g.hex(32)
}

// Token returns a unique 64-character lowercase hexadecimal credential.
func (g *Generator) Token() string {
	return g.hex(64)
}

func (g *Generator) hex(width int) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return fmt.Sprintf("%0*x", width, g.next)
}
