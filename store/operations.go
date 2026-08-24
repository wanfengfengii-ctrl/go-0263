package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/ledger"
	"github.com/olivepress/fruit-intake-gate/task"
)

// LockTask freezes a new intake task from catalogue identifiers, crate seals,
// intake batch, blind codes, resources, thresholds and reviewers. It validates
// the harvest window, cultivar, rule digest and reviewer qualification, and
// rejects any intake batch, crate seal or blind code already held by an open
// task.
func (s *SQLite) LockTask(ctx context.Context, req LockRequest) (*TaskView, error) {
	digest, err := requestDigest(req)
	if err != nil {
		return nil, mapErr(err)
	}
	scope := "lock:" + req.IntakeBatch

	var view *TaskView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		tick, err := nextTick(tx)
		if err != nil {
			return err
		}
		cached, found, err := checkIdempotency(tx, scope, req.OperationNo, digest)
		if err != nil {
			return err
		}
		if found {
			var v TaskView
			if err := json.Unmarshal([]byte(cached), &v); err != nil {
				return err
			}
			view = &v
			return nil
		}

		plot, err := getPlotTx(tx, req.PlotID)
		if err != nil {
			return err
		}
		rule, err := getRuleTx(tx, req.RuleDigest)
		if err != nil {
			return err
		}
		if err := catalog.ValidateLockInput(plot, rule, req.CultivarID, req.HarvestAt, req.ReviewerIDs); err != nil {
			return err
		}

		// Intake batch, crate seals and blind codes are one-time gates.
		if err := checkIntakeBatchFree(tx, req.IntakeBatch); err != nil {
			return err
		}
		if dups := duplicateSeals(tx, req.CrateSeals); len(dups) > 0 {
			return NewCodedError(CodeDuplicateSeal, "crate seal already held by an open task", dups...)
		}
		if dups := duplicateBlindCodes(tx, req.BlindCodes); len(dups) > 0 {
			return NewCodedError(CodeDuplicateBlindCode, "blind code already held by an open task", dups...)
		}

		sn := task.Snapshot{
			PlotID:          req.PlotID,
			CultivarID:      req.CultivarID,
			HarvestAt:       req.HarvestAt,
			IntakeBatch:     req.IntakeBatch,
			CrateSeals:      sortedStrings(req.CrateSeals),
			BlindCodes:      sortedStrings(req.BlindCodes),
			ColorGrades:     append([]catalog.ColorGrade(nil), rule.ColorGrades...),
			Resources:       append([]catalog.Resource(nil), rule.Resources...),
			ScreeningPoints: sortedStrings(rule.ScreeningPoints),
			Thresholds:      req.Thresholds,
			ReviewerIDs:     append([]catalog.ReviewerID(nil), req.ReviewerIDs...),
			RuleDigest:      req.RuleDigest,
		}
		snapshotJSON, err := marshalSnapshot(sn)
		if err != nil {
			return err
		}

		id := task.TaskID("task-" + uuid.NewString())
		_, err = tx.Exec(
			`INSERT INTO tasks(task_id, intake_batch, generation, state, locked_snapshot_json, rule_digest, created_at_tick, version)
			 VALUES(?,?,?,?,?,?,?,1)`,
			string(id), req.IntakeBatch, 1, task.StatePendingSampleConfirm,
			string(snapshotJSON), string(req.RuleDigest), tick)
		if err != nil {
			return errIntakeBatchDuplicate
		}

		for _, seal := range sn.CrateSeals {
			if _, err := tx.Exec(`INSERT INTO crate_gates(task_id, crate_seal) VALUES(?,?)`, string(id), seal); err != nil {
				return errDuplicateSeal
			}
		}
		for i, code := range sn.BlindCodes {
			if _, err := tx.Exec(`INSERT INTO blind_samples(task_id, blind_code, split_index, generation) VALUES(?,?,?,?)`,
				string(id), code, i, 1); err != nil {
				return errDuplicateBlindCode
			}
		}

		if err := insertAudit(tx, uuid.NewString(), string(id), 1, "system", "task-locked", string(id), "", tick); err != nil {
			return err
		}

		v, err := buildTaskView(tx, id)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(v)
		if err := recordIdempotency(tx, scope, req.OperationNo, digest, http.StatusOK, string(body), tick); err != nil {
			return err
		}
		view = v
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// SampleConfirm records two-person receiving confirmation for every sealed
// crate under the current generation.
func (s *SQLite) SampleConfirm(ctx context.Context, id task.TaskID, req SampleConfirmRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpSampleConfirm, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StatePendingSampleConfirm {
			return errInvalidState
		}
		gates := ledger.CrateGates{Gates: make(map[string]*ledger.CrateGate)}
		rows, err := tx.Query(`SELECT crate_seal FROM crate_gates WHERE task_id=? ORDER BY crate_seal`, string(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var seal string
			if err := rows.Scan(&seal); err != nil {
				return err
			}
			gates.Gates[seal] = &ledger.CrateGate{TaskID: id, CrateSeal: seal}
		}
		tick, _ := nextTick(tx)
		if err := gates.ConfirmAll(req.ReviewerA, req.ReviewerB, ledger.Tick(tick)); err != nil {
			return err
		}
		for seal, g := range gates.Gates {
			if _, err := tx.Exec(`UPDATE crate_gates SET confirmed_by_a=?, confirmed_by_b=?, confirmed_tick=? WHERE task_id=? AND crate_seal=?`,
				g.ConfirmedByA, g.ConfirmedByB, int64(g.ConfirmedTick), string(id), seal); err != nil {
				return err
			}
		}
		if _, err := t.Advance(task.StateSplittingSamples); err != nil {
			return err
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, req.ReviewerA, "sample-confirmed", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// SplitSamples creates blind-code split records without revealing the crate
// mapping. The split records already exist from locking; this operation simply
// advances the task past the split stage and must be atomic.
func (s *SQLite) SplitSamples(ctx context.Context, id task.TaskID, req SplitSamplesRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpSplitSamples, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StateSplittingSamples {
			return errInvalidState
		}
		if _, err := t.Advance(task.StateResourcesOccupied); err != nil {
			return err
		}
		tick, _ := nextTick(tx)
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "samples-split", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// RevealSamples maps blind codes to crate seals under the current generation,
// validating that each target crate was confirmed by the receiving gate and
// that each blind sample is revealed at most once.
func (s *SQLite) RevealSamples(ctx context.Context, id task.TaskID, req RevealRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpReveal, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		gen := resolveGeneration(req.Generation, t.Generation)
		if gen != t.Generation {
			return ledger.ErrGenerationMismatch
		}
		confirmed := make(map[string]bool)
		rows, err := tx.Query(`SELECT crate_seal FROM crate_gates WHERE task_id=? AND confirmed_tick IS NOT NULL`, string(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var seal string
			if err := rows.Scan(&seal); err != nil {
				return err
			}
			confirmed[seal] = true
		}
		for _, m := range req.Reveals {
			if !sn.HasBlindCode(m.BlindCode) {
				return ledger.ErrCrateSealUnknown
			}
			if !sn.HasCrateSeal(m.CrateSeal) {
				return ledger.ErrCrateSealUnknown
			}
			sample := ledger.BlindSample{TaskID: id, BlindCode: m.BlindCode, Generation: t.Generation}
			if err := sample.Reveal(m.CrateSeal, t.Generation, confirmed[m.CrateSeal]); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE blind_samples SET revealed_crate_seal=? WHERE task_id=? AND blind_code=? AND revealed_crate_seal IS NULL`,
				m.CrateSeal, string(id), m.BlindCode); err != nil {
				return err
			}
		}
		tick, _ := nextTick(tx)
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "samples-revealed", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// StartResources atomically captures crusher-line, inert-window and test-hole
// leases for every frozen resource, failing with sorted reasons when any
// resource is already held by another open task.
func (s *SQLite) StartResources(ctx context.Context, id task.TaskID, req StartResourcesRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpStartResources, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StateResourcesOccupied {
			return errInvalidState
		}
		tick, _ := nextTick(tx)

		// Detect all busy resources up front for a deterministic, sorted reject.
		var busy []string
		for _, r := range sn.Resources {
			var other string
			err := tx.QueryRow(
				`SELECT task_id FROM resource_leases
				 WHERE resource_type=? AND resource_id=? AND released_tick IS NULL AND task_id<>?`,
				string(r.Kind), r.ID, string(id)).Scan(&other)
			if err == nil {
				busy = append(busy, string(r.Kind)+":"+r.ID)
			} else if err != sql.ErrNoRows {
				return err
			}
		}
		if len(busy) > 0 {
			return NewCodedError(CodeResourceBusy, "resource already held", sortedStrings(busy)...)
		}

		for _, r := range sn.Resources {
			_, err := tx.Exec(
				`INSERT INTO resource_leases(task_id, resource_type, resource_id, generation, start_tick, expire_tick)
				 VALUES(?,?,?,?,?,?)`,
				string(id), string(r.Kind), r.ID, t.Generation, tick, tick+int64(ledger.LeaseDuration))
			if err != nil {
				return ledger.ErrResourceBusy
			}
		}
		if _, err := t.Advance(task.StateMaturityCounting); err != nil {
			return err
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "resources-started", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// MaturityCounts writes full maturity coverage cells and validates integer
// count conservation over the locked color grades and crate seals.
func (s *SQLite) MaturityCounts(ctx context.Context, id task.TaskID, req MaturityCountsRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpMaturityCounts, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StateMaturityCounting {
			return errInvalidState
		}
		gen := resolveGeneration(req.Generation, t.Generation)
		if gen != t.Generation {
			return ledger.ErrGenerationMismatch
		}

		cells := make([]evidence.MaturityCell, 0, len(req.Cells))
		for _, c := range req.Cells {
			cells = append(cells, evidence.MaturityCell{
				TaskID: id, Generation: t.Generation,
				CrateSeal: c.CrateSeal, ColorGrade: c.ColorGrade, Count: c.Count,
			})
		}
		cov := evidence.Coverage{
			CrateSeals:  sn.CrateSeals,
			ColorGrades: sn.ColorGrades,
			Total:       req.Total,
			Cells:       cells,
		}
		if err := evidence.ValidateCoverage(cov); err != nil {
			return err
		}
		for _, c := range req.Cells {
			if _, err := tx.Exec(
				`INSERT INTO maturity_cells(task_id, generation, crate_seal, color_grade, count, version)
				 VALUES(?,?,?,?,?,?)`,
				string(id), t.Generation, c.CrateSeal, string(c.ColorGrade), c.Count, t.Generation); err != nil {
				return err
			}
		}
		tick, _ := nextTick(tx)
		if _, err := t.Advance(task.StateOxidationVerifying); err != nil {
			return err
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "maturity-recorded", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// SubmitReadings parses and records fixed-point physicochemical readings. A
// scripted adapter failure records a retry row and leaves the reading
// unaccepted without releasing leases or advancing state.
func (s *SQLite) SubmitReadings(ctx context.Context, id task.TaskID, req ReadingsRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpReadings, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StateOxidationVerifying {
			return errInvalidState
		}
		gen := resolveGeneration(req.Generation, t.Generation)
		if gen != t.Generation {
			return ledger.ErrGenerationMismatch
		}
		tick, _ := nextTick(tx)

		// Route scripted adapters first: failures become deterministic retries.
		for _, a := range req.Adapters {
			if a.Outcome == "" || a.Outcome == evidence.OutcomeSuccess {
				continue
			}
			if _, err := tx.Exec(
				`INSERT INTO adapter_calls(call_id, task_id, generation, adapter_kind, target_key, attempt_no, planned_tick, outcome, payload_digest)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				uuid.NewString(), string(id), t.Generation, string(a.Kind), a.Target, a.Attempt,
				tick+evidence.RetryBackoffTicks, string(a.Outcome), string(task.DigestPayload(a.Target))); err != nil {
				return err
			}
		}
		if anyAdapterFailed(req.Adapters) {
			if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "adapter-retry", string(id), "", tick); err != nil {
				return err
			}
			return evidence.ErrAdapterFailed
		}

		raw := map[evidence.ReadingKind]string{
			evidence.ReadingAcid:       req.Acid,
			evidence.ReadingPeroxide:   req.Peroxide,
			evidence.ReadingPolyphenol: req.Polyphenol,
			evidence.ReadingMoisture:   req.Moisture,
			evidence.ReadingFruitTemp:  req.FruitTemp,
		}
		for _, k := range evidence.ReadingKinds() {
			scale := readingScale(sn.Thresholds, k)
			fp, err := evidence.ParseFixed(raw[k], scale)
			if err != nil {
				return err
			}
			reason := ""
			if lim, ok := sn.Thresholds.Limit(k.ThresholdKind()); ok && lim.Bounded() {
				if r := (evidence.Reading{Kind: k, Value: fp}).ValidateAgainstThreshold(lim); r != nil {
					reason = "out-of-range"
				}
			}
			if err := insertEvidence(tx, id, t.Generation, evidence.EvidenceReading, string(k), fp.Value, fp.Scale, raw[k], true, reason, tick); err != nil {
				return err
			}
		}
		if _, err := t.Advance(task.StateForeignMatterRetesting); err != nil {
			return err
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "readings-recorded", string(id), "", tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// ForeignMatter records screening findings, moisture repeat checks and the
// affected crate/blind-code/test-hole references, then advances to independent
// review.
func (s *SQLite) ForeignMatter(ctx context.Context, id task.TaskID, req ForeignMatterRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpForeignMatter, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StateForeignMatterRetesting {
			return errInvalidState
		}
		gen := resolveGeneration(req.Generation, t.Generation)
		if gen != t.Generation {
			return ledger.ErrGenerationMismatch
		}
		tick, _ := nextTick(tx)
		reason := req.Finding
		if reason == "" {
			reason = "clear"
		}
		if err := insertEvidence(tx, id, t.Generation, evidence.EvidenceForeignMatter, "screening", 0, 0, req.Finding+req.MoistureRpt, true, reason, tick); err != nil {
			return err
		}
		if _, err := t.Advance(task.StatePendingIndependentReview); err != nil {
			return err
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "foreign-matter-recorded", string(id), reason, tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// Rejudge creates the single current-generation deterioration recheck for the
// affected crate/blind/test-hole set and advances the task to a fresh
// generation. Re-recording an identical recheck is idempotent; a different one
// for the same generation returns a rejudge conflict.
func (s *SQLite) Rejudge(ctx context.Context, id task.TaskID, req RejudgeRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpRejudge, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if !rejudgeAllowed(t.State) {
			return errInvalidState
		}
		gen := resolveGeneration(req.Generation, t.Generation)
		if gen != t.Generation {
			return arbiter.ErrRejudgeConflict
		}
		recheck := arbiter.Recheck{
			TaskID:     id,
			Generation: t.Generation,
			Reason:     req.Reason,
			CrateSeals: sortedStrings(req.Affected.CrateSeals),
			BlindCodes: sortedStrings(req.Affected.BlindCodes),
			TestHoles:  sortedStrings(req.Affected.TestHoles),
		}
		existing, hasExisting := loadRecheck(tx, id, t.Generation)
		if hasExisting {
			if existing.SameAs(recheck) {
				// Idempotent re-record: nothing to do beyond returning state.
				v, err := buildTaskView(tx, id)
				view = v
				return err
			}
			return arbiter.ErrRejudgeConflict
		}
		tick, _ := nextTick(tx)
		seals, _ := json.Marshal(recheck.CrateSeals)
		codes, _ := json.Marshal(recheck.BlindCodes)
		holes, _ := json.Marshal(recheck.TestHoles)
		if _, err := tx.Exec(
			`INSERT INTO rechecks(task_id, generation, reason, crate_seals, blind_codes, test_holes, created_tick)
			 VALUES(?,?,?,?,?,?,?)`,
			string(id), t.Generation, string(req.Reason), string(seals), string(codes), string(holes), tick); err != nil {
			return err
		}
		// Advance to a fresh generation: late evidence for the old generation is
		// recorded as not-accepted and never rewrites current conclusions.
		t.Generation++
		if t.State != task.StatePendingIndependentReview {
			t.State = task.StatePendingIndependentReview
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "rejudged", string(id), string(req.Reason), tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// Review records an independent review for the current generation. A reviewer
// may only review once per generation and two distinct qualified reviewers
// with distinct roles are required before finalization.
func (s *SQLite) Review(ctx context.Context, id task.TaskID, req ReviewRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpReview, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, sn, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if t.State != task.StatePendingIndependentReview && t.State != task.StateColdPressReady {
			return errInvalidState
		}
		if !qualifiedReviewer(sn, req.ReviewerID) {
			return catalog.ErrReviewerNotQualified
		}
		set := arbiter.ReviewSet{TaskID: id, Generation: t.Generation}
		rows, err := tx.Query(`SELECT reviewer_id, role, decision FROM reviews WHERE task_id=? AND generation=?`, string(id), t.Generation)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r arbiter.Review
			if err := rows.Scan(&r.ReviewerID, &r.Role, &r.Decision); err != nil {
				return err
			}
			r.TaskID, r.Generation = id, t.Generation
			set.Reviews = append(set.Reviews, r)
		}
		review := arbiter.Review{
			TaskID: id, Generation: t.Generation, ReviewerID: req.ReviewerID,
			Role: req.Role, Decision: req.Decision,
			EvidenceDigest: string(task.DigestPayload(string(id) + ":" + fmt.Sprint(t.Generation))),
		}
		if err := set.Add(review); err != nil {
			return err
		}
		tick, _ := nextTick(tx)
		if _, err := tx.Exec(
			`INSERT INTO reviews(task_id, generation, reviewer_id, role, decision, evidence_digest, created_tick)
			 VALUES(?,?,?,?,?,?,?)`,
			string(id), t.Generation, req.ReviewerID, string(req.Role), string(req.Decision), review.EvidenceDigest, tick); err != nil {
			return err
		}
		if set.HasIndependentApproval() && t.State != task.StateColdPressReady {
			if _, err := t.Advance(task.StateColdPressReady); err != nil {
				return err
			}
		}
		if err := saveTaskTx(tx, t, sn); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, req.ReviewerID, "reviewed", string(id), string(req.Decision), tick); err != nil {
			return err
		}
		return finalizeView(tx, scope, req.OperationNo, digest, id, tick, &view)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// Finalize competes for the single terminal outcome through an optimistic
// version check plus a unique final row. Exactly one of cold-press permission,
// isolation or cancellation can commit.
func (s *SQLite) Finalize(ctx context.Context, id task.TaskID, req FinalizeRequest) (*TaskView, error) {
	digest, _ := requestDigest(req)
	scope := task.IdempotencyScope(task.OpFinalize, id)

	var view *TaskView
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if v, done, err := replayIdempotent(tx, scope, req.OperationNo, digest); err != nil || done {
			view = v
			return err
		}
		t, _, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return task.ErrTerminalState
		}
		if req.Kind != task.FinalCancellation &&
			t.State != task.StateColdPressReady && t.State != task.StatePendingIndependentReview {
			return errInvalidState
		}
		hasRecheck := countRechecks(tx, id) > 0

		reviews := arbiter.ReviewSet{TaskID: id, Generation: t.Generation}
		rows, err := tx.Query(`SELECT reviewer_id, role, decision FROM reviews WHERE task_id=? AND generation=?`, string(id), t.Generation)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r arbiter.Review
			if err := rows.Scan(&r.ReviewerID, &r.Role, &r.Decision); err != nil {
				return err
			}
			r.TaskID, r.Generation = id, t.Generation
			reviews.Reviews = append(reviews.Reviews, r)
		}

		if err := arbiter.ValidateFinalize(req.Kind, reviews, hasRecheck); err != nil {
			return err
		}

		cred := arbiter.BuildCredential(id, t.Generation, req.Kind, string(task.DigestPayload(fmt.Sprint(reviews.Reviews))))
		nextState := terminalState(req.Kind)

		res, err := tx.Exec(
			`UPDATE tasks SET final_kind=?, final_credential=?, state=?, version=version+1
			 WHERE task_id=? AND version=? AND final_kind IS NULL`,
			string(req.Kind), cred.Digest, nextState, string(id), t.Version)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return arbiter.ErrFinalConflict
		}
		tick, _ := nextTick(tx)
		if err := insertEvidence(tx, id, t.Generation, evidence.EvidenceFinal, string(req.Kind), 0, 0, cred.Digest, true, "", tick); err != nil {
			return err
		}
		if err := insertAudit(tx, uuid.NewString(), string(id), t.Generation, "system", "finalized", string(id), string(req.Kind), tick); err != nil {
			return err
		}
		v, err := buildTaskView(tx, id)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(v)
		if err := recordIdempotency(tx, scope, req.OperationNo, digest, http.StatusOK, string(body), tick); err != nil {
			return err
		}
		view = v
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return view, nil
}

// --- small helpers shared by operations ---

func replayIdempotent(tx *sql.Tx, scope, opNo, digest string) (*TaskView, bool, error) {
	cached, found, err := checkIdempotency(tx, scope, opNo, digest)
	if err != nil {
		return nil, true, err
	}
	if !found {
		return nil, false, nil
	}
	var v TaskView
	if err := json.Unmarshal([]byte(cached), &v); err != nil {
		return nil, true, err
	}
	return &v, true, nil
}

func finalizeView(tx *sql.Tx, scope, opNo, digest string, id task.TaskID, tick int64, out **TaskView) error {
	v, err := buildTaskView(tx, id)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(v)
	if err := recordIdempotency(tx, scope, opNo, digest, http.StatusOK, string(body), tick); err != nil {
		return err
	}
	*out = v
	return nil
}

// checkIntakeBatchFree mirrors the idx_tasks_intake_open partial unique index:
// only an open (non-finalized) task holds a batch. A cancelled or otherwise
// finalized task releases its batch number so the same batch can be re-locked,
// e.g. after a crate-seal recording error.
func checkIntakeBatchFree(tx *sql.Tx, batch string) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE intake_batch=? AND final_kind IS NULL`, batch).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errIntakeBatchDuplicate
	}
	return nil
}

func duplicateSeals(tx *sql.Tx, seals []string) []string {
	var dups []string
	for _, seal := range seals {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM crate_gates WHERE crate_seal=?`, seal).Scan(&n); err == nil && n > 0 {
			dups = append(dups, seal)
		}
	}
	return sortedStrings(dups)
}

func duplicateBlindCodes(tx *sql.Tx, codes []string) []string {
	var dups []string
	for _, code := range codes {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM blind_samples WHERE blind_code=?`, code).Scan(&n); err == nil && n > 0 {
			dups = append(dups, code)
		}
	}
	return sortedStrings(dups)
}

func readingScale(th catalog.Thresholds, k evidence.ReadingKind) int {
	if lim, ok := th.Limit(k.ThresholdKind()); ok {
		return lim.Scale
	}
	return 0
}

func anyAdapterFailed(adapters []AdapterInput) bool {
	for _, a := range adapters {
		if a.Outcome != "" && a.Outcome != evidence.OutcomeSuccess {
			return true
		}
	}
	return false
}

func insertEvidence(tx *sql.Tx, id task.TaskID, gen task.Generation, kind evidence.EvidenceKind, subject string, value int64, scale int, raw string, accepted bool, reason string, tick int64) error {
	acc := 0
	if accepted {
		acc = 1
	}
	_, err := tx.Exec(
		`INSERT INTO evidence_versions(task_id, generation, evidence_kind, subject_key, version, fixed_value, unit_scale, raw_digest, accepted, reason_code, created_tick)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		string(id), gen, string(kind), subject, nextEvidenceVersion(tx, id, gen, kind, subject),
		value, scale, string(task.DigestPayload(raw)), acc, reason, tick)
	return err
}

func nextEvidenceVersion(tx *sql.Tx, id task.TaskID, gen task.Generation, kind evidence.EvidenceKind, subject string) int {
	var v int
	err := tx.QueryRow(
		`SELECT COALESCE(MAX(version),0) FROM evidence_versions
		 WHERE task_id=? AND generation=? AND evidence_kind=? AND subject_key=?`,
		string(id), gen, string(kind), subject).Scan(&v)
	if err != nil {
		return 1
	}
	return v + 1
}

func rejudgeAllowed(s task.State) bool {
	switch s {
	case task.StateOxidationVerifying, task.StateForeignMatterRetesting, task.StatePendingIndependentReview:
		return true
	default:
		return false
	}
}

func countRechecks(tx *sql.Tx, id task.TaskID) int {
	var n int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM rechecks WHERE task_id=?`, string(id)).Scan(&n)
	return n
}

func qualifiedReviewer(sn task.Snapshot, reviewerID string) bool {
	for _, id := range sn.ReviewerIDs {
		if string(id) == reviewerID {
			return true
		}
	}
	return false
}

func terminalState(k task.FinalKind) task.State {
	switch k {
	case task.FinalColdPress:
		return task.StateColdPressed
	case task.FinalIsolation:
		return task.StateQualityIsolated
	default:
		return task.StateCancelled
	}
}
