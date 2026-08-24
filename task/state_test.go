package task

import "testing"

func TestStateProgression(t *testing.T) {
	seq := []State{
		StatePendingLock,
		StatePendingSampleConfirm,
		StateSplittingSamples,
		StateResourcesOccupied,
		StateMaturityCounting,
		StateOxidationVerifying,
		StateForeignMatterRetesting,
		StatePendingIndependentReview,
		StateColdPressReady,
		StateColdPressed,
	}
	for i := 1; i < len(seq); i++ {
		if !CanTransition(seq[i-1], seq[i]) {
			t.Fatalf("expected transition %s -> %s to be allowed", seq[i-1], seq[i])
		}
	}
}

func TestCannotSkipEvidenceStages(t *testing.T) {
	if CanTransition(StatePendingLock, StateMaturityCounting) {
		t.Fatal("skipping evidence stages must be rejected")
	}
}

func TestCancellationFromNonTerminal(t *testing.T) {
	for _, s := range []State{StatePendingLock, StateSplittingSamples, StateColdPressReady} {
		if !CanTransition(s, StateCancelled) {
			t.Fatalf("expected cancellation allowed from %s", s)
		}
	}
}

func TestTerminalStatesRejectTransition(t *testing.T) {
	for _, s := range []State{StateColdPressed, StateQualityIsolated, StateCancelled} {
		if !IsTerminal(s) {
			t.Fatalf("state %s should be terminal", s)
		}
		if CanTransition(s, StateColdPressReady) {
			t.Fatalf("terminal state %s must reject transitions", s)
		}
	}
}

func TestAdvance(t *testing.T) {
	task := &Task{ID: "t1", State: StatePendingLock}
	if _, err := task.Advance(StatePendingSampleConfirm); err != nil {
		t.Fatalf("advance failed: %v", err)
	}
	if _, err := task.Advance(StatePendingLock); err == nil {
		t.Fatal("backward transition must be rejected")
	}
	task.State = StateColdPressed
	if _, err := task.Advance(StateQualityIsolated); err == nil {
		t.Fatal("terminal transition must be rejected")
	}
}
