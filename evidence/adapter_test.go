package evidence

import (
	"errors"
	"testing"
)

func TestScriptedAdapterOutcomes(t *testing.T) {
	a := ScriptedAdapter{
		Kind: AdapterNearInfrared,
		Outcomes: map[string]AdapterOutcome{
			ScriptKey("th-1", 1): OutcomeReject,
			ScriptKey("th-1", 2): OutcomeDisconnect,
			ScriptKey("th-1", 3): OutcomeTimeout,
			ScriptKey("th-1", 4): OutcomeMalformed,
		},
	}
	for attempt, want := range map[int]AdapterOutcome{
		1: OutcomeReject, 2: OutcomeDisconnect, 3: OutcomeTimeout, 4: OutcomeMalformed,
	} {
		out, err := a.Measure("th-1", attempt)
		if out != want {
			t.Fatalf("attempt %d outcome = %s, want %s", attempt, out, want)
		}
		if !errors.Is(err, ErrAdapterFailed) {
			t.Fatalf("attempt %d error = %v, want ErrAdapterFailed", attempt, err)
		}
	}
	// Unscripted pairs succeed.
	if out, err := a.Measure("th-1", 99); out != OutcomeSuccess || err != nil {
		t.Fatalf("unscripted attempt = %s, %v; want success", out, err)
	}
}

func TestPlanRetryDeterministic(t *testing.T) {
	prev := AdapterCall{
		CallID:      "c1",
		TaskID:      "t1",
		Generation:  1,
		AdapterKind: AdapterTitration,
		TargetKey:   "th-1",
		AttemptNo:   1,
		PlannedTick: 10,
		Outcome:     OutcomeTimeout,
	}
	next := PlanRetry(prev)
	if next.AttemptNo != 2 {
		t.Fatalf("attempt = %d, want 2", next.AttemptNo)
	}
	if next.PlannedTick != 10+RetryBackoffTicks {
		t.Fatalf("planned tick = %d, want %d", next.PlannedTick, 10+RetryBackoffTicks)
	}
	if next.TargetKey != "th-1" || next.AdapterKind != AdapterTitration {
		t.Fatalf("retry must retain target and adapter kind")
	}
}
