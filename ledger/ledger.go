// Package ledger maintains the blind-sample and resource-occupancy ledger:
// delayed blind-code reveal, crate mapping, crusher-line/inert-window/
// test-hole leases and logical-clock expiry rules.
package ledger

import (
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// Tick is a logical-clock unit. All lease and evidence timestamps are
// expressed in ticks so recovery is deterministic after restart.
type Tick int64

// LogicalClock advances a monotonically increasing tick counter.
type LogicalClock struct {
	now Tick
}

// NewClock returns a logical clock starting at zero.
func NewClock() *LogicalClock { return &LogicalClock{} }

// Now returns the current tick without advancing it.
func (c *LogicalClock) Now() Tick { return c.now }

// Advance increments the logical clock and returns the new tick.
func (c *LogicalClock) Advance() Tick {
	c.now++
	return c.now
}

// Lease is a time-bounded resource lease with an optional release tick.
type Lease struct {
	TaskID       task.TaskID
	ResourceKind catalog.ResourceKind
	ResourceID   string
	Generation   task.Generation
	StartTick    Tick
	ExpireTick   Tick
	ReleasedTick *Tick
}

// IsActive reports whether the lease is currently held (not expired and not
// released) at the given logical tick.
func (l *Lease) IsActive(at Tick) bool {
	if l.ReleasedTick != nil {
		return false
	}
	return at >= l.StartTick && at < l.ExpireTick
}

// LeaseDuration is the fixed lease window in ticks before expiry.
const LeaseDuration Tick = 100

// NewLease creates an active lease beginning at the current clock tick.
func NewLease(clock *LogicalClock, t task.TaskID, kind catalog.ResourceKind, resourceID string, gen task.Generation) *Lease {
	start := clock.Advance()
	return &Lease{
		TaskID:       t,
		ResourceKind: kind,
		ResourceID:   resourceID,
		Generation:   gen,
		StartTick:    start,
		ExpireTick:   start + LeaseDuration,
	}
}

// CrateGate records two-person receiving confirmation for a sealed crate.
type CrateGate struct {
	TaskID        task.TaskID
	CrateSeal     string
	ConfirmedByA  string
	ConfirmedByB  string
	ConfirmedTick Tick
}

// BlindSample records a blind-code sample split entry. The crate mapping is
// delayed: RevealedCrateSeal is empty until an explicit reveal matches the
// current generation.
type BlindSample struct {
	TaskID            task.TaskID
	BlindCode         string
	SplitIndex        int
	RevealedCrateSeal string
	Generation        task.Generation
}
