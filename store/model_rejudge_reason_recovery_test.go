package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_RejudgeRetainsEvidenceReasons(t *testing.T) {
	ctx := context.Background()

	seedCatalog := func(t *testing.T, s *SQLite) catalog.Rule {
		t.Helper()
		c := catalog.SeedCatalog()
		for _, p := range c.Plots() {
			if err := s.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		rules := c.Rules()
		for _, r := range rules {
			if err := s.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
		}
		if len(rules) == 0 {
			t.Fatal("seed catalog has no rules")
		}
		return rules[0]
	}

	openStore := func(t *testing.T) (*SQLite, string, catalog.Rule) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "olivepress.db")
		s, err := NewSQLite(path)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		return s, path, seedCatalog(t, s)
	}

	advanceToOxidation := func(t *testing.T, s *SQLite, batch string, rule catalog.Rule) task.TaskID {
		t.Helper()
		view, err := s.LockTask(ctx, LockRequest{
			OperationNo: "op-lock-" + batch,
			PlotID:      "plot-picual-1",
			CultivarID:  "picual",
			HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			IntakeBatch: batch,
			CrateSeals:  []string{batch + "-s1", batch + "-s2"},
			BlindCodes:  []string{batch + "-b1", batch + "-b2"},
			Thresholds:  rule.Thresholds,
			ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
			RuleDigest:  rule.Digest,
		})
		if err != nil {
			t.Fatalf("lock: %v", err)
		}
		id := view.Task.ID
		if _, err := s.SampleConfirm(ctx, id, SampleConfirmRequest{OperationNo: "op-confirm-" + batch, ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
			t.Fatalf("sample confirm: %v", err)
		}
		if _, err := s.SplitSamples(ctx, id, SplitSamplesRequest{OperationNo: "op-split-" + batch}); err != nil {
			t.Fatalf("split samples: %v", err)
		}
		if _, err := s.StartResources(ctx, id, StartResourcesRequest{OperationNo: "op-resources-" + batch}); err != nil {
			t.Fatalf("start resources: %v", err)
		}
		if _, err := s.MaturityCounts(ctx, id, MaturityCountsRequest{
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
		}); err != nil {
			t.Fatalf("maturity counts: %v", err)
		}
		return id
	}

	cases := []struct {
		name                 string
		batch                string
		recordForeignMatter  bool
		rejudge              bool
		insertLateUnaccepted bool
		wantGeneration       task.Generation
		wantReasons          []string
	}{
		{
			name:           "current oxidation breach reason remains visible",
			batch:          "model-current-oxidation",
			wantGeneration: 1,
			wantReasons:    []string{"out-of-range"},
		},
		{
			name:                "current generation reasons stay sorted",
			batch:               "model-current-sorted",
			recordForeignMatter: true,
			wantGeneration:      1,
			wantReasons:         []string{"foreign-matter-doubt", "out-of-range"},
		},
		{
			name:                 "rejudge recovery keeps accepted history and ignores unaccepted late evidence",
			batch:                "model-rejudge",
			recordForeignMatter:  true,
			rejudge:              true,
			insertLateUnaccepted: true,
			wantGeneration:       2,
			wantReasons:          []string{"foreign-matter-doubt", "out-of-range", "oxidation-breach"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, path, rule := openStore(t)
			id := advanceToOxidation(t, s, tc.batch, rule)
			if _, err := s.SubmitReadings(ctx, id, ReadingsRequest{
				OperationNo: "op-readings-" + tc.batch,
				Acid:        "0.42",
				Peroxide:    "20.01",
				Polyphenol:  "220",
				Moisture:    "32.5",
				FruitTemp:   "21.0",
			}); err != nil {
				t.Fatalf("submit readings: %v", err)
			}
			if tc.recordForeignMatter {
				if _, err := s.ForeignMatter(ctx, id, ForeignMatterRequest{
					OperationNo: "op-foreign-" + tc.batch,
					Finding:     string(arbiter.RejudgeForeignMatter),
				}); err != nil {
					t.Fatalf("foreign matter: %v", err)
				}
			}
			if tc.rejudge {
				if _, err := s.Rejudge(ctx, id, RejudgeRequest{
					OperationNo: "op-rejudge-" + tc.batch,
					Reason:      arbiter.RejudgeOxidation,
					Affected: ForeignRefs{
						CrateSeals: []string{tc.batch + "-s1"},
						BlindCodes: []string{tc.batch + "-b1"},
						TestHoles:  []string{"th-1"},
					},
				}); err != nil {
					t.Fatalf("rejudge: %v", err)
				}
			}
			if tc.insertLateUnaccepted {
				if _, err := s.db.Exec(
					`INSERT INTO evidence_versions(task_id, generation, evidence_kind, subject_key, version, fixed_value, unit_scale, raw_digest, accepted, reason_code, created_tick)
					 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
					string(id), 1, string(evidence.EvidenceReading), "late-old-generation", 1, int64(9999), 2, "late-digest", 0, "aaa-late-unaccepted", int64(999),
				); err != nil {
					t.Fatalf("insert late unaccepted evidence: %v", err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatalf("close store: %v", err)
			}

			reopened, err := NewSQLite(path)
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			defer reopened.Close()
			got, err := reopened.GetTaskView(ctx, id)
			if err != nil {
				t.Fatalf("get recovered task: %v", err)
			}
			if got.Generation != tc.wantGeneration {
				t.Fatalf("generation = %d, want %d", got.Generation, tc.wantGeneration)
			}
			if !slices.Equal(got.Reasons, tc.wantReasons) {
				t.Fatalf("reasons = %v, want %v", got.Reasons, tc.wantReasons)
			}
		})
	}
}
