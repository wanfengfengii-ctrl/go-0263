package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_IntakeBatchUniquenessOnlyAppliesToOpenTasks(t *testing.T) {
	ctx := context.Background()

	codeOf := func(t *testing.T, err error) string {
		t.Helper()
		var ce *store.CodedError
		if !errors.As(err, &ce) {
			t.Fatalf("expected CodedError, got %T %v", err, err)
		}
		return ce.Code
	}

	seed := func(t *testing.T, s *store.SQLite) catalog.Rule {
		t.Helper()
		c := catalog.SeedCatalog()
		var rule catalog.Rule
		for _, p := range c.Plots() {
			if err := s.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		for _, r := range c.Rules() {
			if err := s.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
			if rule.ID == "" {
				rule = r
			}
		}
		if rule.ID == "" {
			t.Fatal("seed catalog did not provide a rule")
		}
		return rule
	}

	lockRequest := func(rule catalog.Rule, batch, label string) store.LockRequest {
		return store.LockRequest{
			OperationNo: "op-lock-" + label,
			PlotID:      "plot-picual-1",
			CultivarID:  "picual",
			HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			IntakeBatch: batch,
			CrateSeals:  []string{label + "-seal-a", label + "-seal-b"},
			BlindCodes:  []string{label + "-blind-a", label + "-blind-b"},
			Thresholds:  rule.Thresholds,
			ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
			RuleDigest:  rule.Digest,
		}
	}

	advanceToReview := func(t *testing.T, s *store.SQLite, id task.TaskID, label string, seals []string) {
		t.Helper()
		if _, err := s.SampleConfirm(ctx, id, store.SampleConfirmRequest{
			OperationNo: "op-confirm-" + label,
			ReviewerA:   "rev-a",
			ReviewerB:   "rev-b",
		}); err != nil {
			t.Fatalf("sample confirm: %v", err)
		}
		if _, err := s.SplitSamples(ctx, id, store.SplitSamplesRequest{
			OperationNo: "op-split-" + label,
		}); err != nil {
			t.Fatalf("split samples: %v", err)
		}
		if _, err := s.StartResources(ctx, id, store.StartResourcesRequest{
			OperationNo: "op-start-resources-" + label,
		}); err != nil {
			t.Fatalf("start resources: %v", err)
		}
		if _, err := s.MaturityCounts(ctx, id, store.MaturityCountsRequest{
			OperationNo: "op-maturity-" + label,
			Total: map[string]int{
				seals[0]: 100,
				seals[1]: 100,
			},
			Cells: []store.MaturityCellInput{
				{CrateSeal: seals[0], ColorGrade: catalog.ColorGreen, Count: 60},
				{CrateSeal: seals[0], ColorGrade: catalog.ColorTurning, Count: 20},
				{CrateSeal: seals[0], ColorGrade: catalog.ColorPurpleBlack, Count: 15},
				{CrateSeal: seals[0], ColorGrade: catalog.ColorDamaged, Count: 4},
				{CrateSeal: seals[0], ColorGrade: catalog.ColorMoldy, Count: 1},
				{CrateSeal: seals[1], ColorGrade: catalog.ColorGreen, Count: 65},
				{CrateSeal: seals[1], ColorGrade: catalog.ColorTurning, Count: 18},
				{CrateSeal: seals[1], ColorGrade: catalog.ColorPurpleBlack, Count: 12},
				{CrateSeal: seals[1], ColorGrade: catalog.ColorDamaged, Count: 3},
				{CrateSeal: seals[1], ColorGrade: catalog.ColorMoldy, Count: 2},
			},
		}); err != nil {
			t.Fatalf("maturity counts: %v", err)
		}
		if _, err := s.SubmitReadings(ctx, id, store.ReadingsRequest{
			OperationNo: "op-readings-" + label,
			Acid:        "0.42",
			Peroxide:    "12.50",
			Polyphenol:  "220",
			Moisture:    "32.5",
			FruitTemp:   "21.0",
		}); err != nil {
			t.Fatalf("submit readings: %v", err)
		}
		if _, err := s.ForeignMatter(ctx, id, store.ForeignMatterRequest{
			OperationNo: "op-foreign-matter-" + label,
			Finding:     "clear",
		}); err != nil {
			t.Fatalf("foreign matter: %v", err)
		}
	}

	type finishFunc func(*testing.T, *store.SQLite, task.TaskID, string, []string)

	cases := []struct {
		name     string
		key      string
		finish   finishFunc
		wantCode string
	}{
		{
			name:     "open task still blocks same batch",
			key:      "open",
			wantCode: store.CodeIntakeBatchDuplicate,
		},
		{
			name: "cancelled task releases same batch",
			key:  "cancelled",
			finish: func(t *testing.T, s *store.SQLite, id task.TaskID, label string, _ []string) {
				t.Helper()
				view, err := s.Finalize(ctx, id, store.FinalizeRequest{
					OperationNo: "op-finalize-" + label,
					Kind:        task.FinalCancellation,
				})
				if err != nil {
					t.Fatalf("finalize cancellation: %v", err)
				}
				if view.Task.FinalKind != task.FinalCancellation {
					t.Fatalf("final kind = %s, want %s", view.Task.FinalKind, task.FinalCancellation)
				}
			},
		},
		{
			name: "cold-press task releases same batch",
			key:  "coldpress",
			finish: func(t *testing.T, s *store.SQLite, id task.TaskID, label string, seals []string) {
				t.Helper()
				advanceToReview(t, s, id, label, seals)
				if _, err := s.Review(ctx, id, store.ReviewRequest{
					OperationNo: "op-review-primary-" + label,
					ReviewerID:  "rev-a",
					Role:        arbiter.RolePrimary,
					Decision:    arbiter.DecisionApprove,
				}); err != nil {
					t.Fatalf("primary review: %v", err)
				}
				if _, err := s.Review(ctx, id, store.ReviewRequest{
					OperationNo: "op-review-secondary-" + label,
					ReviewerID:  "rev-b",
					Role:        arbiter.RoleSecondary,
					Decision:    arbiter.DecisionApprove,
				}); err != nil {
					t.Fatalf("secondary review: %v", err)
				}
				view, err := s.Finalize(ctx, id, store.FinalizeRequest{
					OperationNo: "op-finalize-" + label,
					Kind:        task.FinalColdPress,
				})
				if err != nil {
					t.Fatalf("finalize cold press: %v", err)
				}
				if view.Task.FinalKind != task.FinalColdPress {
					t.Fatalf("final kind = %s, want %s", view.Task.FinalKind, task.FinalColdPress)
				}
			},
		},
		{
			name: "isolated task releases same batch",
			key:  "isolation",
			finish: func(t *testing.T, s *store.SQLite, id task.TaskID, label string, seals []string) {
				t.Helper()
				advanceToReview(t, s, id, label, seals)
				if _, err := s.Rejudge(ctx, id, store.RejudgeRequest{
					OperationNo: "op-rejudge-" + label,
					Reason:      arbiter.RejudgeOxidation,
					Affected: store.ForeignRefs{
						CrateSeals: []string{seals[0]},
						BlindCodes: []string{label + "-blind-a"},
						TestHoles:  []string{"th-1"},
					},
				}); err != nil {
					t.Fatalf("rejudge: %v", err)
				}
				view, err := s.Finalize(ctx, id, store.FinalizeRequest{
					OperationNo: "op-finalize-" + label,
					Kind:        task.FinalIsolation,
				})
				if err != nil {
					t.Fatalf("finalize isolation: %v", err)
				}
				if view.Task.FinalKind != task.FinalIsolation {
					t.Fatalf("final kind = %s, want %s", view.Task.FinalKind, task.FinalIsolation)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := store.NewSQLite(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer s.Close()
			rule := seed(t, s)

			batch := "model-relock-" + tc.key
			originalLabel := "original-" + tc.key
			replacementLabel := "replacement-" + tc.key
			originalReq := lockRequest(rule, batch, originalLabel)
			original, err := s.LockTask(ctx, originalReq)
			if err != nil {
				t.Fatalf("original lock: %v", err)
			}
			if tc.finish != nil {
				tc.finish(t, s, original.Task.ID, originalLabel, originalReq.CrateSeals)
			}

			replacementReq := lockRequest(rule, batch, replacementLabel)
			replacement, err := s.LockTask(ctx, replacementReq)
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("replacement lock succeeded, want %s", tc.wantCode)
				}
				if got := codeOf(t, err); got != tc.wantCode {
					t.Fatalf("replacement lock error = %s, want %s", got, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("replacement lock after terminal task: %v", err)
			}
			if replacement.Task.ID == original.Task.ID {
				t.Fatalf("replacement reused original task id %s", replacement.Task.ID)
			}
			if replacement.Task.IntakeBatch != batch {
				t.Fatalf("replacement batch = %s, want %s", replacement.Task.IntakeBatch, batch)
			}
			if got, want := replacement.Snapshot.CrateSeals[0], replacementReq.CrateSeals[0]; got != want {
				t.Fatalf("replacement first crate seal = %s, want %s", got, want)
			}
			if got, want := replacement.Snapshot.BlindCodes[0], replacementReq.BlindCodes[0]; got != want {
				t.Fatalf("replacement first blind code = %s, want %s", got, want)
			}
		})
	}
}
