package evidence

import (
	"errors"
	"fmt"

	"github.com/olivepress/fruit-intake-gate/task"
)

// AdapterKind identifies an instrument adapter used to collect a reading.
type AdapterKind string

// The three scripted instrument adapters on the cold-press line.
const (
	AdapterNearInfrared  AdapterKind = "near-infrared"
	AdapterTitration     AdapterKind = "titration"
	AdapterMoistureMeter AdapterKind = "moisture-meter"
)

// AdapterOutcome is the deterministic outcome of one instrument call.
type AdapterOutcome string

// Outcomes a scripted adapter can produce.
const (
	OutcomeSuccess    AdapterOutcome = "success"
	OutcomeReject     AdapterOutcome = "reject"
	OutcomeDisconnect AdapterOutcome = "disconnect"
	OutcomeTimeout    AdapterOutcome = "timeout"
	OutcomeMalformed  AdapterOutcome = "malformed"
)

// AdapterCall records one instrument invocation attempt. A failed call yields
// a pending retry row with a determinable attempt number, target and planned
// tick; it never produces accepted evidence nor releases an active lease.
type AdapterCall struct {
	CallID      string
	TaskID      task.TaskID
	Generation  task.Generation
	AdapterKind AdapterKind
	TargetKey   string
	AttemptNo   int
	PlannedTick int64
	Outcome     AdapterOutcome
	PayloadHash string
}

// ErrAdapterFailed is returned when an adapter call does not succeed.
var ErrAdapterFailed = errors.New("adapter: instrument call failed")

// Adapter abstracts an instrument that measures a fresh-fruit reading.
type Adapter interface {
	// Measure performs one attempt against the target. A non-nil error is
	// returned for reject, disconnect, timeout and malformed outcomes.
	Measure(target string, attempt int) (AdapterOutcome, error)
}

// ScriptedAdapter is a deterministic adapter whose outcome for a given
// (target, attempt) pair is fixed by a script map. Unscripted pairs succeed.
type ScriptedAdapter struct {
	Kind     AdapterKind
	Outcomes map[string]AdapterOutcome
}

// ScriptKey builds the script lookup key for a target and attempt.
func ScriptKey(target string, attempt int) string {
	return fmt.Sprintf("%s#%d", target, attempt)
}

// Measure returns the scripted outcome, defaulting to success.
func (a ScriptedAdapter) Measure(target string, attempt int) (AdapterOutcome, error) {
	out := OutcomeSuccess
	if v, ok := a.Outcomes[ScriptKey(target, attempt)]; ok {
		out = v
	}
	if out == OutcomeSuccess {
		return out, nil
	}
	return out, fmt.Errorf("%w: %s %s on attempt %d", ErrAdapterFailed, a.Kind, out, attempt)
}

// RetryBackoffTicks is the fixed logical-time backoff before a failed adapter
// call may be retried.
const RetryBackoffTicks int64 = 7

// PlanRetry computes the next deterministic retry call for a failed attempt:
// the attempt number increments and the planned tick advances by the fixed
// backoff, making retries reproducible after restart.
func PlanRetry(prev AdapterCall) AdapterCall {
	next := prev
	next.AttemptNo++
	next.PlannedTick += RetryBackoffTicks
	next.Outcome = ""
	return next
}
