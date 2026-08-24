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

func seedTestCatalog(t *testing.T, s *SQLite) {
	t.Helper()
	c := catalog.SeedCatalog()
	ctx := context.Background()
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
}

func seedRule() catalog.Rule {
	c := catalog.SeedCatalog()
	for _, r := range c.Rules() {
		return r
	}
	return catalog.Rule{}
}

func validLock(batch string) LockRequest {
	r := seedRule()
	return LockRequest{
		OperationNo: "op-lock-" + batch,
		PlotID:      "plot-picual-1",
		CultivarID:  "picual",
		HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		IntakeBatch: batch,
		CrateSeals:  []string{batch + "-s1", batch + "-s2"},
		BlindCodes:  []string{batch + "-b1", batch + "-b2"},
		Thresholds:  r.Thresholds,
		ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
		RuleDigest:  r.Digest,
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce *CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CodedError, got %T %v", err, err)
	}
	return ce.Code
}

func newMemory(t *testing.T) *SQLite {
	t.Helper()
	s, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seedTestCatalog(t, s)
	return s
}

func TestLockTaskValidAndRejections(t *testing.T) {
	ctx := context.Background()
	s := newMemory(t)
	defer s.Close()

	view, err := s.LockTask(ctx, validLock("batch-1"))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if view.Task.State != task.StatePendingSampleConfirm {
		t.Fatalf("state = %s, want pending-sample-confirm", view.Task.State)
	}
	if len(view.BlindSamples) != 2 || len(view.CrateGates) != 2 {
		t.Fatalf("expected 2 blind samples and 2 crate gates, got %d/%d", len(view.BlindSamples), len(view.CrateGates))
	}

	// Stale rule digest.
	stale := validLock("batch-2")
	stale.RuleDigest = "deadbeef"
	if _, err := s.LockTask(ctx, stale); codeOf(t, err) != CodeStaleRuleDigest {
		t.Fatalf("want ERR_STALE_RULE_DIGEST, got %v", err)
	}

	// Harvest window mismatch.
	early := validLock("batch-3")
	early.HarvestAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.LockTask(ctx, early); codeOf(t, err) != CodePlotCultivarWindow {
		t.Fatalf("want ERR_PLOT_CULTIVAR_WINDOW, got %v", err)
	}

	// Duplicate crate seal.
	dup := validLock("batch-4")
	dup.CrateSeals = []string{"batch-1-s1", "batch-4-s9"}
	if _, err := s.LockTask(ctx, dup); codeOf(t, err) != CodeDuplicateSeal {
		t.Fatalf("want ERR_DUPLICATE_SEAL, got %v", err)
	}

	// Duplicate intake batch (fresh operation number, same batch).
	dupBatch := validLock("batch-1")
	dupBatch.OperationNo = "op-lock-batch-1-dup"
	if _, err := s.LockTask(ctx, dupBatch); codeOf(t, err) != CodeIntakeBatchDuplicate {
		t.Fatalf("want ERR_INTAKE_BATCH_DUPLICATE, got %v", err)
	}
}

func TestIdempotentReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	s := newMemory(t)
	defer s.Close()

	req := validLock("batch-idem")
	v1, err := s.LockTask(ctx, req)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	// Identical replay returns the same task.
	v2, err := s.LockTask(ctx, req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if v1.Task.ID != v2.Task.ID {
		t.Fatalf("replay returned a different task: %s vs %s", v1.Task.ID, v2.Task.ID)
	}

	// Conflicting content under the same operation number is rejected.
	conflict := validLock("batch-idem")
	conflict.CrateSeals = []string{"batch-idem-x1", "batch-idem-x2"}
	if _, err := s.LockTask(ctx, conflict); codeOf(t, err) != CodeOperationConflict {
		t.Fatalf("want ERR_OPERATION_CONFLICT, got %v", err)
	}
}

func TestFullHappyPathToColdPress(t *testing.T) {
	ctx := context.Background()
	s := newMemory(t)
	defer s.Close()

	id := lockAndAdvanceToReview(t, s, ctx, "batch-happy")

	// Two independent reviews.
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r1", ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); err != nil {
		t.Fatalf("review 1: %v", err)
	}
	view, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r2", ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove})
	if err != nil {
		t.Fatalf("review 2: %v", err)
	}
	if view.State != task.StateColdPressReady {
		t.Fatalf("state = %s, want cold-press-ready", view.State)
	}

	fin, err := s.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-fin", Kind: task.FinalColdPress})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if fin.Task.FinalKind != task.FinalColdPress || fin.Final.Credential == "" {
		t.Fatalf("final = %+v", fin.Final)
	}

	// Terminal-state operations are rejected.
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r3", ReviewerID: "rev-c", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); codeOf(t, err) != CodeTerminalState {
		t.Fatalf("want ERR_TERMINAL_STATE after finalize, got %v", err)
	}
}

func TestRejudgeIsolationAndLateEvidence(t *testing.T) {
	ctx := context.Background()
	s := newMemory(t)
	defer s.Close()

	id := lockAndAdvanceToReview(t, s, ctx, "batch-rejudge")

	// Create one deterioration recheck; this advances the generation.
	_, err := s.Rejudge(ctx, id, RejudgeRequest{
		OperationNo: "op-rej",
		Reason:      arbiter.RejudgeOxidation,
		Affected:    ForeignRefs{CrateSeals: []string{"batch-rejudge-s1"}, BlindCodes: []string{"batch-rejudge-b1"}, TestHoles: []string{"th-1"}},
	})
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}

	// A conflicting rejudge for the now-old generation is rejected.
	_, err = s.Rejudge(ctx, id, RejudgeRequest{
		OperationNo: "op-rej2",
		Generation:  1,
		Reason:      arbiter.RejudgeMaturity,
		Affected:    ForeignRefs{CrateSeals: []string{"batch-rejudge-s2"}},
	})
	if codeOf(t, err) != CodeRejudgeGenerationConflict {
		t.Fatalf("want ERR_REJUDGE_GENERATION_CONFLICT, got %v", err)
	}

	// Isolation is the only cold-press-adjacent outcome with a recheck present.
	fin, err := s.Finalize(ctx, id, FinalizeRequest{OperationNo: "op-fin-iso", Kind: task.FinalIsolation})
	if err != nil {
		t.Fatalf("finalize isolation: %v", err)
	}
	if fin.Task.FinalKind != task.FinalIsolation {
		t.Fatalf("final kind = %s, want isolation", fin.Task.FinalKind)
	}
}

func TestReviewRoleOverlapAndGenerationMismatch(t *testing.T) {
	ctx := context.Background()
	s := newMemory(t)
	defer s.Close()

	id := lockAndAdvanceToReview(t, s, ctx, "batch-role")

	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r1", ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); err != nil {
		t.Fatalf("review 1: %v", err)
	}
	// Same role overlap.
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r2", ReviewerID: "rev-b", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove}); codeOf(t, err) != CodeRoleOverlap {
		t.Fatalf("want ERR_ROLE_OVERLAP, got %v", err)
	}
	// Unqualified reviewer.
	if _, err := s.Review(ctx, id, ReviewRequest{OperationNo: "op-r3", ReviewerID: "rev-x", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove}); codeOf(t, err) != CodeReviewerNotQualified {
		t.Fatalf("want ERR_REVIEWER_NOT_QUALIFIED, got %v", err)
	}
}

// lockAndAdvanceToReview runs the full pipeline up to pending-independent-review
// and returns the task id.
func lockAndAdvanceToReview(t *testing.T, s *SQLite, ctx context.Context, batch string) task.TaskID {
	t.Helper()
	view, err := s.LockTask(ctx, validLock(batch))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	id := view.Task.ID

	if _, err := s.SampleConfirm(ctx, id, SampleConfirmRequest{OperationNo: "op-sc", ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
		t.Fatalf("sample-confirm: %v", err)
	}
	if _, err := s.SplitSamples(ctx, id, SplitSamplesRequest{OperationNo: "op-split"}); err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := s.StartResources(ctx, id, StartResourcesRequest{OperationNo: "op-res"}); err != nil {
		t.Fatalf("start-resources: %v", err)
	}

	mc := MaturityCountsRequest{
		OperationNo: "op-maturity",
		Total:       map[string]int{batch + "-s1": 100, batch + "-s2": 100},
		Cells: []MaturityCellInput{
			{batch + "-s1", catalog.ColorGreen, 60},
			{batch + "-s1", catalog.ColorTurning, 20},
			{batch + "-s1", catalog.ColorPurpleBlack, 15},
			{batch + "-s1", catalog.ColorDamaged, 4},
			{batch + "-s1", catalog.ColorMoldy, 1},
			{batch + "-s2", catalog.ColorGreen, 65},
			{batch + "-s2", catalog.ColorTurning, 18},
			{batch + "-s2", catalog.ColorPurpleBlack, 12},
			{batch + "-s2", catalog.ColorDamaged, 3},
			{batch + "-s2", catalog.ColorMoldy, 2},
		},
	}
	if _, err := s.MaturityCounts(ctx, id, mc); err != nil {
		t.Fatalf("maturity: %v", err)
	}

	rd := ReadingsRequest{
		OperationNo: "op-read",
		Acid:        "0.42",
		Peroxide:    "12.50",
		Polyphenol:  "220",
		Moisture:    "32.5",
		FruitTemp:   "21.0",
	}
	if _, err := s.SubmitReadings(ctx, id, rd); err != nil {
		t.Fatalf("readings: %v", err)
	}

	if _, err := s.ForeignMatter(ctx, id, ForeignMatterRequest{OperationNo: "op-fm", Finding: "clear"}); err != nil {
		t.Fatalf("foreign-matter: %v", err)
	}
	return id
}
