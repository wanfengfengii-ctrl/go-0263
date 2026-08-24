package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/task"
)

// querier abstracts *sql.DB and *sql.Tx so the task view can be rebuilt from
// either a live transaction or the database connection.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// GetTaskView returns the fully recovered state of a task, rebuilt from
// persisted rows only. No in-memory state is authoritative across restarts.
func (s *SQLite) GetTaskView(ctx context.Context, id task.TaskID) (*TaskView, error) {
	v, err := buildTaskView(s.db, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return v, nil
}

// buildTaskView reconstructs a task aggregate plus all related rows: gates,
// blind samples, leases, coverage cells, readings, adapter retries, recheck,
// reviews and the final credential.
func buildTaskView(q querier, id task.TaskID) (*TaskView, error) {
	t, sn, err := loadTaskView(q, id)
	if err != nil {
		return nil, err
	}

	view := &TaskView{
		Task:       *t,
		Snapshot:   sn,
		State:      t.State,
		Generation: t.Generation,
	}

	view.CrateGates, err = loadCrateGates(q, id)
	if err != nil {
		return nil, err
	}
	view.BlindSamples, err = loadBlindSamples(q, id)
	if err != nil {
		return nil, err
	}
	view.Leases, err = loadLeases(q, id)
	if err != nil {
		return nil, err
	}
	view.Maturity, err = loadMaturity(q, id)
	if err != nil {
		return nil, err
	}
	view.Readings, err = loadReadings(q, id)
	if err != nil {
		return nil, err
	}
	view.Retries, err = loadRetries(q, id)
	if err != nil {
		return nil, err
	}
	if rc, ok := loadRecheck(q, id, t.Generation); ok {
		view.Recheck = &RecheckView{
			Reason:     rc.Reason,
			CrateSeals: rc.CrateSeals,
			BlindCodes: rc.BlindCodes,
			TestHoles:  rc.TestHoles,
		}
	}
	view.Reviews, err = loadReviews(q, id, t.Generation)
	if err != nil {
		return nil, err
	}
	if t.FinalKind != "" {
		view.Final = &FinalView{Kind: t.FinalKind, Credential: t.FinalCredential}
	}
	view.Reasons = loadReasons(q, id)
	return view, nil
}

func loadTaskView(q querier, id task.TaskID) (*task.Task, task.Snapshot, error) {
	var (
		t            task.Task
		snapshotJSON string
		createdTick  int64
	)
	err := q.QueryRow(
		`SELECT task_id, intake_batch, generation, state, locked_snapshot_json,
		        rule_digest, created_at_tick, COALESCE(final_kind,''), COALESCE(final_credential,''), version
		 FROM tasks WHERE task_id=?`, string(id)).
		Scan(&t.ID, &t.IntakeBatch, &t.Generation, &t.State, &snapshotJSON,
			&t.RuleDigest, &createdTick, &t.FinalKind, &t.FinalCredential, &t.Version)
	if err != nil {
		return nil, task.Snapshot{}, ErrNotFound
	}
	sn, err := unmarshalSnapshot([]byte(snapshotJSON))
	if err != nil {
		return nil, task.Snapshot{}, err
	}
	return &t, sn, nil
}

func loadCrateGates(q querier, id task.TaskID) ([]CrateGateView, error) {
	rows, err := q.Query(`SELECT crate_seal, confirmed_by_a, confirmed_by_b, confirmed_tick FROM crate_gates WHERE task_id=? ORDER BY crate_seal`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CrateGateView
	for rows.Next() {
		var g CrateGateView
		var a, b sql.NullString
		var tick sql.NullInt64
		if err := rows.Scan(&g.CrateSeal, &a, &b, &tick); err != nil {
			return nil, err
		}
		g.ConfirmedByA, g.ConfirmedByB = a.String, b.String
		g.Confirmed = tick.Valid
		out = append(out, g)
	}
	return out, rows.Err()
}

func loadBlindSamples(q querier, id task.TaskID) ([]BlindSampleView, error) {
	rows, err := q.Query(`SELECT blind_code, split_index, revealed_crate_seal FROM blind_samples WHERE task_id=? ORDER BY split_index`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlindSampleView
	for rows.Next() {
		var b BlindSampleView
		var revealed sql.NullString
		if err := rows.Scan(&b.BlindCode, &b.SplitIndex, &revealed); err != nil {
			return nil, err
		}
		b.RevealedCrateSeal = revealed.String
		out = append(out, b)
	}
	return out, rows.Err()
}

func loadLeases(q querier, id task.TaskID) ([]LeaseView, error) {
	rows, err := q.Query(`SELECT resource_type, resource_id, start_tick, expire_tick, released_tick FROM resource_leases WHERE task_id=? ORDER BY resource_type, resource_id`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseView
	for rows.Next() {
		var l LeaseView
		var released sql.NullInt64
		if err := rows.Scan(&l.ResourceType, &l.ResourceID, &l.StartTick, &l.ExpireTick, &released); err != nil {
			return nil, err
		}
		l.Released = released.Valid
		out = append(out, l)
	}
	return out, rows.Err()
}

func loadMaturity(q querier, id task.TaskID) ([]MaturityCellView, error) {
	rows, err := q.Query(`SELECT crate_seal, color_grade, count FROM maturity_cells WHERE task_id=? ORDER BY crate_seal, color_grade`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaturityCellView
	for rows.Next() {
		var m MaturityCellView
		if err := rows.Scan(&m.CrateSeal, &m.ColorGrade, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func loadReadings(q querier, id task.TaskID) ([]ReadingView, error) {
	rows, err := q.Query(
		`SELECT subject_key, fixed_value, unit_scale FROM evidence_versions
		 WHERE task_id=? AND evidence_kind=? AND accepted=1
		 ORDER BY created_tick, subject_key`, string(id), string(evidence.EvidenceReading))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReadingView
	for rows.Next() {
		var r ReadingView
		if err := rows.Scan(&r.Kind, &r.Value.Value, &r.Value.Scale); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadRetries(q querier, id task.TaskID) ([]AdapterCallView, error) {
	rows, err := q.Query(`SELECT adapter_kind, target_key, attempt_no, outcome, planned_tick FROM adapter_calls WHERE task_id=? ORDER BY attempt_no, adapter_kind`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdapterCallView
	for rows.Next() {
		var a AdapterCallView
		if err := rows.Scan(&a.AdapterKind, &a.TargetKey, &a.AttemptNo, &a.Outcome, &a.PlannedTick); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func loadReviews(q querier, id task.TaskID, gen task.Generation) ([]ReviewView, error) {
	rows, err := q.Query(`SELECT reviewer_id, role, decision FROM reviews WHERE task_id=? AND generation=? ORDER BY reviewer_id`, string(id), gen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewView
	for rows.Next() {
		var r ReviewView
		if err := rows.Scan(&r.ReviewerID, &r.Role, &r.Decision); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadRecheck reads the recheck for a generation from either a transaction or
// the database connection.
func loadRecheck(q querier, id task.TaskID, gen task.Generation) (arbiter.Recheck, bool) {
	var r arbiter.Recheck
	var seals, codes, holes string
	err := q.QueryRow(
		`SELECT reason, crate_seals, blind_codes, test_holes FROM rechecks WHERE task_id=? AND generation=?`,
		string(id), gen).Scan(&r.Reason, &seals, &codes, &holes)
	if err != nil {
		return r, false
	}
	r.TaskID, r.Generation = id, gen
	_ = jsonUnmarshal(seals, &r.CrateSeals)
	_ = jsonUnmarshal(codes, &r.BlindCodes)
	_ = jsonUnmarshal(holes, &r.TestHoles)
	return r, true
}

// loadReasons collects deterministically sorted reason codes from accepted
// out-of-range evidence and any recheck, for stable reason-list serialization.
func loadReasons(q querier, id task.TaskID) []string {
	var reasons []string
	rows, err := q.Query(
		`SELECT reason_code FROM evidence_versions
		 WHERE task_id=? AND accepted=1 AND reason_code IS NOT NULL AND reason_code<>''`,
		string(id))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r string
			if rows.Scan(&r) == nil {
				reasons = append(reasons, r)
			}
		}
	}
	sort.Strings(reasons)
	return reasons
}

func jsonUnmarshal(s string, v *[]string) error {
	return json.Unmarshal([]byte(s), v)
}
