// Package task models the fresh-fruit intake task aggregate: its state
// machine, task generation, locked snapshot, idempotent operation handling
// and terminal-state barriers.
package task

import "errors"

// State is a task lifecycle state in the cold-press quality gate.
type State string

// Ordered task states. Except for cancellation, a task may only advance
// forward through the business evidence stages.
const (
	StatePendingLock              State = "pending-lock"
	StatePendingSampleConfirm     State = "pending-sample-confirm"
	StateSplittingSamples         State = "splitting-samples"
	StateResourcesOccupied        State = "resources-occupied"
	StateMaturityCounting         State = "maturity-counting"
	StateOxidationVerifying       State = "oxidation-verifying"
	StateForeignMatterRetesting   State = "foreign-matter-retesting"
	StatePendingIndependentReview State = "pending-independent-review"
	StateColdPressReady           State = "cold-press-ready"
	StateColdPressed              State = "cold-pressed"
	StateQualityIsolated          State = "quality-isolated"
	StateCancelled                State = "cancelled"
)

// order maps each state to its position in the forward progression.
var order = map[State]int{
	StatePendingLock:              0,
	StatePendingSampleConfirm:     1,
	StateSplittingSamples:         2,
	StateResourcesOccupied:        3,
	StateMaturityCounting:         4,
	StateOxidationVerifying:       5,
	StateForeignMatterRetesting:   6,
	StatePendingIndependentReview: 7,
	StateColdPressReady:           8,
	StateColdPressed:              9,
	StateQualityIsolated:          9,
	StateCancelled:                9,
}

// IsTerminal reports whether a task in state s can no longer transition.
func IsTerminal(s State) bool {
	switch s {
	case StateColdPressed, StateQualityIsolated, StateCancelled:
		return true
	default:
		return false
	}
}

// ErrTerminalState is returned when a mutation targets a terminal task.
var ErrTerminalState = errors.New("task: terminal state")

// ErrInvalidTransition is returned when a state transition is not permitted.
var ErrInvalidTransition = errors.New("task: invalid state transition")

// CanTransition reports whether a task may advance from s to next.
// Cancellation is permitted from any non-terminal state; otherwise a task
// must advance forward through the ordered evidence stages.
func CanTransition(s, next State) bool {
	if IsTerminal(s) {
		return false
	}
	if next == StateCancelled {
		return true
	}
	cur, ok := order[s]
	if !ok {
		return false
	}
	nxt, ok := order[next]
	if !ok {
		return false
	}
	return nxt > cur && nxt <= cur+1
}

// Advance returns the next state after s, or an error when the transition
// is not permitted.
func (s State) Advance(next State) (State, error) {
	if !CanTransition(s, next) {
		if IsTerminal(s) {
			return s, ErrTerminalState
		}
		return s, ErrInvalidTransition
	}
	return next, nil
}
