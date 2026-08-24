package task

import "github.com/olivepress/fruit-intake-gate/catalog"

// TaskID identifies an intake task.
type TaskID string

// Generation is a monotonically increasing task generation (代次).
type Generation int64

// FinalKind is the single terminal outcome of a finalized task.
type FinalKind string

// Terminal outcome kinds produced by the arbiter's single write barrier.
const (
	FinalColdPress    FinalKind = "cold-press"
	FinalIsolation    FinalKind = "isolation"
	FinalCancellation FinalKind = "cancellation"
)

// Task is the intake task aggregate. It freezes a rule digest, an intake
// batch, and a locked snapshot at creation, then advances through the
// state machine by generation.
type Task struct {
	ID              TaskID             `json:"task_id"`
	IntakeBatch     string             `json:"intake_batch"`
	Generation      Generation         `json:"generation"`
	State           State              `json:"state"`
	RuleDigest      catalog.RuleDigest `json:"rule_digest"`
	FinalKind       FinalKind          `json:"final_kind,omitempty"`
	FinalCredential string             `json:"final_credential,omitempty"`
	Version         int64              `json:"version"`
}

// IsTerminal reports whether the task has reached a terminal state.
func (t *Task) IsTerminal() bool {
	return IsTerminal(t.State)
}

// Advance applies a state transition under the current generation and
// returns the previous state on error.
func (t *Task) Advance(next State) (State, error) {
	prev := t.State
	s, err := t.State.Advance(next)
	if err != nil {
		return prev, err
	}
	t.State = s
	return s, nil
}
