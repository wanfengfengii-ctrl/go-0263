package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_OperationNoScopedToBusinessOperation(t *testing.T) {
	ctx := context.Background()

	newSeededStore := func(t *testing.T) (*store.Memory, catalog.Rule) {
		t.Helper()
		s := store.NewMemory()
		c := catalog.SeedCatalog()
		for _, p := range c.Plots() {
			if err := s.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		rules := c.Rules()
		if len(rules) == 0 {
			t.Fatalf("seed catalog has no rules")
		}
		for _, r := range rules {
			if err := s.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
		}
		return s, rules[0]
	}

	lockRequest := func(rule catalog.Rule, batch string) store.LockRequest {
		return store.LockRequest{
			OperationNo: "lock-" + batch,
			PlotID:      "plot-picual-1",
			CultivarID:  "picual",
			HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			IntakeBatch: batch,
			CrateSeals:  []string{batch + "-s1", batch + "-s2"},
			BlindCodes:  []string{batch + "-b1", batch + "-b2"},
			Thresholds:  rule.Thresholds,
			ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
			RuleDigest:  rule.Digest,
		}
	}

	lockTask := func(t *testing.T, s *store.Memory, rule catalog.Rule, batch string) task.TaskID {
		t.Helper()
		view, err := s.LockTask(ctx, lockRequest(rule, batch))
		if err != nil {
			t.Fatalf("lock task: %v", err)
		}
		return view.Task.ID
	}

	codeOf := func(t *testing.T, err error) string {
		t.Helper()
		var coded *store.CodedError
		if !errors.As(err, &coded) {
			t.Fatalf("expected CodedError, got %T %v", err, err)
		}
		return coded.Code
	}

	const reusedOperationNo = "client-reused-operation"

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "same interface same body replays",
			run: func(t *testing.T) {
				s, rule := newSeededStore(t)
				defer s.Close()
				id := lockTask(t, s, rule, "scope-replay")
				req := store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-b",
				}

				first, err := s.SampleConfirm(ctx, id, req)
				if err != nil {
					t.Fatalf("sample confirm: %v", err)
				}
				second, err := s.SampleConfirm(ctx, id, req)
				if err != nil {
					t.Fatalf("sample confirm replay: %v", err)
				}
				if first.Task.ID != second.Task.ID || second.State != task.StateSplittingSamples {
					t.Fatalf("replay returned task/state %s/%s, want %s/%s", second.Task.ID, second.State, first.Task.ID, task.StateSplittingSamples)
				}
			},
		},
		{
			name: "same interface different body conflicts",
			run: func(t *testing.T) {
				s, rule := newSeededStore(t)
				defer s.Close()
				id := lockTask(t, s, rule, "scope-conflict")

				if _, err := s.SampleConfirm(ctx, id, store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-b",
				}); err != nil {
					t.Fatalf("sample confirm: %v", err)
				}
				_, err := s.SampleConfirm(ctx, id, store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-c",
				})
				if code := codeOf(t, err); code != store.CodeOperationConflict {
					t.Fatalf("code = %s, want %s", code, store.CodeOperationConflict)
				}
			},
		},
		{
			name: "different interfaces reuse operation number",
			run: func(t *testing.T) {
				s, rule := newSeededStore(t)
				defer s.Close()
				id := lockTask(t, s, rule, "scope-interface")

				if _, err := s.SampleConfirm(ctx, id, store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-b",
				}); err != nil {
					t.Fatalf("sample confirm: %v", err)
				}
				view, err := s.SplitSamples(ctx, id, store.SplitSamplesRequest{OperationNo: reusedOperationNo})
				if err != nil {
					t.Fatalf("split samples with reused operation_no: %v", err)
				}
				if view.State != task.StateResourcesOccupied {
					t.Fatalf("state = %s, want %s", view.State, task.StateResourcesOccupied)
				}
			},
		},
		{
			name: "different tasks reuse operation number",
			run: func(t *testing.T) {
				s, rule := newSeededStore(t)
				defer s.Close()
				firstID := lockTask(t, s, rule, "scope-task-a")
				secondID := lockTask(t, s, rule, "scope-task-b")

				if _, err := s.SampleConfirm(ctx, firstID, store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-b",
				}); err != nil {
					t.Fatalf("first sample confirm: %v", err)
				}
				view, err := s.SampleConfirm(ctx, secondID, store.SampleConfirmRequest{
					OperationNo: reusedOperationNo,
					ReviewerA:   "rev-a",
					ReviewerB:   "rev-b",
				})
				if err != nil {
					t.Fatalf("second task sample confirm with reused operation_no: %v", err)
				}
				if view.Task.ID != secondID || view.State != task.StateSplittingSamples {
					t.Fatalf("second task returned task/state %s/%s, want %s/%s", view.Task.ID, view.State, secondID, task.StateSplittingSamples)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
