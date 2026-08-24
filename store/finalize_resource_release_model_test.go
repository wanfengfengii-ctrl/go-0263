package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_FinalizeResourceLeaseRelease(t *testing.T) {
	ctx := context.Background()

	codeOf := func(t *testing.T, err error) string {
		t.Helper()
		var ce *CodedError
		if !errors.As(err, &ce) {
			t.Fatalf("expected CodedError, got %T %v", err, err)
		}
		return ce.Code
	}

	newStore := func(t *testing.T) *SQLite {
		t.Helper()
		s, err := NewSQLite(":memory:")
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		c := catalog.SeedCatalog()
		for _, p := range c.Plots() {
			if err := s.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		for _, r := range c.Rules() {
			if err := s.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
		}
		return s
	}

	lockRequest := func(t *testing.T, batch string) LockRequest {
		t.Helper()
		rules := catalog.SeedCatalog().Rules()
		if len(rules) != 1 {
			t.Fatalf("seed catalog rules = %d, want 1", len(rules))
		}
		return LockRequest{
			OperationNo: "op-lock-" + batch,
			PlotID:      "plot-picual-1",
			CultivarID:  "picual",
			HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			IntakeBatch: batch,
			CrateSeals:  []string{batch + "-s1", batch + "-s2"},
			BlindCodes:  []string{batch + "-b1", batch + "-b2"},
			Thresholds:  rules[0].Thresholds,
			ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
			RuleDigest:  rules[0].Digest,
		}
	}

	advanceToReview := func(t *testing.T, s *SQLite, batch string) task.TaskID {
		t.Helper()
		view, err := s.LockTask(ctx, lockRequest(t, batch))
		if err != nil {
			t.Fatalf("lock %s: %v", batch, err)
		}
		id := view.Task.ID
		if _, err := s.SampleConfirm(ctx, id, SampleConfirmRequest{OperationNo: "op-confirm-" + batch, ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
			t.Fatalf("sample confirm %s: %v", batch, err)
		}
		if _, err := s.SplitSamples(ctx, id, SplitSamplesRequest{OperationNo: "op-split-" + batch}); err != nil {
			t.Fatalf("split %s: %v", batch, err)
		}
		if _, err := s.StartResources(ctx, id, StartResourcesRequest{OperationNo: "op-start-" + batch}); err != nil {
			t.Fatalf("start resources %s: %v", batch, err)
		}
		maturity := MaturityCountsRequest{
			OperationNo: "op-maturity-" + batch,
			Total:       map[string]int{batch + "-s1": 100, batch + "-s2": 100},
			Cells: []MaturityCellInput{
				{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorGreen, Count: 60},
				{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorTurning, Count: 20},
				{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorPurpleBlack, Count: 15},
				{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorDamaged, Count: 4},
				{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorMoldy, Count: 1},
				{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorGreen, Count: 65},
				{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorTurning, Count: 18},
				{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorPurpleBlack, Count: 12},
				{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorDamaged, Count: 3},
				{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorMoldy, Count: 2},
			},
		}
		if _, err := s.MaturityCounts(ctx, id, maturity); err != nil {
			t.Fatalf("maturity %s: %v", batch, err)
		}
		readings := ReadingsRequest{
			OperationNo: "op-readings-" + batch,
			Acid:        "0.42",
			Peroxide:    "12.50",
			Polyphenol:  "220",
			Moisture:    "32.5",
			FruitTemp:   "21.0",
		}
		if _, err := s.SubmitReadings(ctx, id, readings); err != nil {
			t.Fatalf("readings %s: %v", batch, err)
		}
		if _, err := s.ForeignMatter(ctx, id, ForeignMatterRequest{OperationNo: "op-foreign-" + batch, Finding: "clear"}); err != nil {
			t.Fatalf("foreign matter %s: %v", batch, err)
		}
		return id
	}

	advanceToResourceStart := func(t *testing.T, s *SQLite, batch string) task.TaskID {
		t.Helper()
		view, err := s.LockTask(ctx, lockRequest(t, batch))
		if err != nil {
			t.Fatalf("lock later %s: %v", batch, err)
		}
		id := view.Task.ID
		if _, err := s.SampleConfirm(ctx, id, SampleConfirmRequest{OperationNo: "op-confirm-" + batch, ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
			t.Fatalf("sample confirm later %s: %v", batch, err)
		}
		if _, err := s.SplitSamples(ctx, id, SplitSamplesRequest{OperationNo: "op-split-" + batch}); err != nil {
			t.Fatalf("split later %s: %v", batch, err)
		}
		return id
	}

	cases := []struct {
		name               string
		prepare            func(*testing.T, *SQLite, task.TaskID, string)
		finalKind          task.FinalKind
		wantFinalizeCode   string
		wantLaterStartCode string
		wantReleased       bool
	}{
		{
			name: "cold press commit releases crusher inert window and test holes",
			prepare: func(t *testing.T, s *SQLite, id task.TaskID, batch string) {
				t.Helper()
				if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-review-a-" + batch, ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); err != nil {
					t.Fatalf("primary review %s: %v", batch, err)
				}
				if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-review-b-" + batch, ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove}); err != nil {
					t.Fatalf("secondary review %s: %v", batch, err)
				}
			},
			finalKind:    task.FinalColdPress,
			wantReleased: true,
		},
		{
			name: "isolation commit releases leases held by the rechecked task",
			prepare: func(t *testing.T, s *SQLite, id task.TaskID, batch string) {
				t.Helper()
				_, err := s.Rejudge(ctx, id, RejudgeRequest{
					OperationNo: "op-rejudge-" + batch,
					Reason:      arbiter.RejudgeOxidation,
					Affected: ForeignRefs{
						CrateSeals: []string{batch + "-s1"},
						BlindCodes: []string{batch + "-b1"},
						TestHoles:  []string{"th-1"},
					},
				})
				if err != nil {
					t.Fatalf("rejudge %s: %v", batch, err)
				}
			},
			finalKind:    task.FinalIsolation,
			wantReleased: true,
		},
		{
			name: "rejected finalization leaves active leases conflicting",
			prepare: func(t *testing.T, s *SQLite, id task.TaskID, batch string) {
				t.Helper()
				if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-review-a-" + batch, ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); err != nil {
					t.Fatalf("single review %s: %v", batch, err)
				}
			},
			finalKind:          task.FinalColdPress,
			wantFinalizeCode:   CodeInvalidRequest,
			wantLaterStartCode: CodeResourceBusy,
			wantReleased:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			defer s.Close()

			batch := "batch-" + t.Name()
			id := advanceToReview(t, s, batch)
			tc.prepare(t, s, id, batch)

			view, err := s.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-final-" + batch, Kind: tc.finalKind})
			if tc.wantFinalizeCode == "" {
				if err != nil {
					t.Fatalf("finalize %s: %v", batch, err)
				}
				if view.Final == nil || view.Final.Credential == "" {
					t.Fatalf("finalize %s returned no final credential: %+v", batch, view.Final)
				}
				if _, err := s.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-final-again-" + batch, Kind: task.FinalCancellation}); codeOf(t, err) != CodeTerminalState {
					t.Fatalf("second finalize %s should hit terminal barrier, got %v", batch, err)
				}
			} else {
				if err == nil {
					t.Fatalf("finalize %s unexpectedly succeeded", batch)
				}
				if got := codeOf(t, err); got != tc.wantFinalizeCode {
					t.Fatalf("finalize code = %s, want %s", got, tc.wantFinalizeCode)
				}
				view, err = s.GetTaskView(ctx, id)
				if err != nil {
					t.Fatalf("load task after failed finalize %s: %v", batch, err)
				}
				if view.Final != nil || view.Task.IsTerminal() {
					t.Fatalf("failed finalize %s changed terminal state: state=%s final=%+v", batch, view.State, view.Final)
				}
			}

			if len(view.Leases) != 4 {
				t.Fatalf("lease count after finalize %s = %d, want 4", batch, len(view.Leases))
			}
			for _, lease := range view.Leases {
				if lease.Released != tc.wantReleased {
					t.Fatalf("lease %s:%s released=%v, want %v", lease.ResourceType, lease.ResourceID, lease.Released, tc.wantReleased)
				}
			}

			laterID := advanceToResourceStart(t, s, batch+"-later")
			laterView, err := s.StartResources(ctx, laterID, StartResourcesRequest{OperationNo: "op-start-" + batch + "-later"})
			if tc.wantLaterStartCode == "" {
				if err != nil {
					t.Fatalf("later start resources after %s: %v", batch, err)
				}
				if len(laterView.Leases) != 4 {
					t.Fatalf("later lease count after %s = %d, want 4", batch, len(laterView.Leases))
				}
			} else if got := codeOf(t, err); got != tc.wantLaterStartCode {
				t.Fatalf("later start code = %s, want %s", got, tc.wantLaterStartCode)
			}
		})
	}
}
