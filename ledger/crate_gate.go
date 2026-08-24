package ledger

import (
	"errors"

	"github.com/olivepress/fruit-intake-gate/task"
)

// Sentinel errors for the receiving gate.
var (
	ErrSameReviewer      = errors.New("ledger: receiving confirmation requires two distinct reviewers")
	ErrMissingReviewer   = errors.New("ledger: receiving confirmation requires two reviewers")
	ErrCrateNotConfirmed = errors.New("ledger: crate not confirmed")
	ErrAlreadyRevealed   = errors.New("ledger: blind sample already revealed")
	ErrCrateSealUnknown  = errors.New("ledger: crate seal not frozen for task")
)

// Confirm records a two-person receiving confirmation for a sealed crate. The
// two reviewers must be distinct and non-empty; on success the gate becomes
// confirmed at the given tick.
func (g *CrateGate) Confirm(reviewerA, reviewerB string, tick Tick) error {
	if reviewerA == "" || reviewerB == "" {
		return ErrMissingReviewer
	}
	if reviewerA == reviewerB {
		return ErrSameReviewer
	}
	if g.ConfirmedTick != 0 {
		return nil // already confirmed is idempotent
	}
	g.ConfirmedByA = reviewerA
	g.ConfirmedByB = reviewerB
	g.ConfirmedTick = tick
	return nil
}

// Confirmed reports whether the gate has been confirmed by two people.
func (g *CrateGate) Confirmed() bool {
	return g.ConfirmedTick != 0
}

// CrateGates is a set of crate gates for one task.
type CrateGates struct {
	TaskID task.TaskID
	Gates  map[string]*CrateGate
}

// ConfirmAll confirms every gate with the same two distinct reviewers.
func (c *CrateGates) ConfirmAll(reviewerA, reviewerB string, tick Tick) error {
	if reviewerA == "" || reviewerB == "" {
		return ErrMissingReviewer
	}
	if reviewerA == reviewerB {
		return ErrSameReviewer
	}
	for _, g := range c.Gates {
		if g.ConfirmedTick == 0 {
			g.ConfirmedByA = reviewerA
			g.ConfirmedByB = reviewerB
			g.ConfirmedTick = tick
		}
	}
	return nil
}

// AllConfirmed reports whether every gate is confirmed.
func (c *CrateGates) AllConfirmed() bool {
	for _, g := range c.Gates {
		if !g.Confirmed() {
			return false
		}
	}
	return true
}
