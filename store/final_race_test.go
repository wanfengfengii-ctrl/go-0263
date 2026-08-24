package store

import (
	"context"
	"sync"
	"testing"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestFinalRaceAndRecovery(t *testing.T) {
	ctx := context.Background()
	s, path := newFileStore(t)

	id := lockAndAdvanceToReview(t, s, ctx, "batch-final-race")
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r1", ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); err != nil {
		t.Fatalf("review 1: %v", err)
	}
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r2", ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove}); err != nil {
		t.Fatalf("review 2: %v", err)
	}

	kinds := []task.FinalKind{task.FinalColdPress, task.FinalIsolation, task.FinalCancellation}
	var wg sync.WaitGroup
	errs := make([]error, len(kinds))
	views := make([]*TaskView, len(kinds))
	start := make(chan struct{})

	for i, kind := range kinds {
		wg.Add(1)
		go func(i int, kind task.FinalKind) {
			defer wg.Done()
			<-start
			views[i], errs[i] = s.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-fin-" + string(rune('a'+i)), Kind: kind})
		}(i, kind)
	}
	close(start)
	wg.Wait()

	committed := 0
	for i, err := range errs {
		if err == nil {
			committed++
			if views[i] == nil || views[i].Final == nil || views[i].Final.Credential == "" {
				t.Fatalf("successful finalize missing credential")
			}
		} else {
			c := codeOf(t, err)
			if c != CodeTerminalState && c != CodeInvalidRequest && c != CodeInvalidState {
				t.Fatalf("unexpected losing error code: %s (%v)", c, err)
			}
		}
	}
	if committed != 1 {
		t.Fatalf("committed = %d, want exactly 1", committed)
	}

	// Restart the service and confirm the single persisted final conclusion.
	s.Close()
	s2, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	view, err := s2.GetTaskView(ctx, id)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if view.Final == nil || view.Final.Credential == "" {
		t.Fatalf("final conclusion not recovered after restart")
	}
	if !view.Task.IsTerminal() {
		t.Fatalf("recovered task should be terminal, state=%s", view.State)
	}

	// Terminal-state rejection after restart.
	if _, err := s2.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-fin-again", Kind: task.FinalCancellation}); codeOf(t, err) != CodeTerminalState {
		t.Fatalf("after restart want ERR_TERMINAL_STATE, got %v", err)
	}
}
