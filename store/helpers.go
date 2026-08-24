package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// withTx runs fn inside a single SQLite transaction, rolling back on error and
// committing on success. Every mutating store operation funnels through here
// so partial writes are impossible.
func (s *SQLite) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// marshalSnapshot renders a locked snapshot deterministically. time.Time fields
// marshal as RFC3339, keeping serialized snapshots stable across restarts.
func marshalSnapshot(sn task.Snapshot) ([]byte, error) {
	return json.Marshal(sn)
}

// unmarshalSnapshot reverses marshalSnapshot.
func unmarshalSnapshot(b []byte) (task.Snapshot, error) {
	var sn task.Snapshot
	if err := json.Unmarshal(b, &sn); err != nil {
		return sn, err
	}
	return sn, nil
}

// loadTaskTx loads a task row and its locked snapshot inside tx.
func loadTaskTx(tx *sql.Tx, id task.TaskID) (*task.Task, task.Snapshot, error) {
	var (
		t            task.Task
		snapshotJSON string
		createdTick  int64
	)
	err := tx.QueryRow(
		`SELECT task_id, intake_batch, generation, state, locked_snapshot_json,
		        rule_digest, created_at_tick, COALESCE(final_kind,''), COALESCE(final_credential,''), version
		 FROM tasks WHERE task_id=?`, string(id)).
		Scan(&t.ID, &t.IntakeBatch, &t.Generation, &t.State, &snapshotJSON,
			&t.RuleDigest, &createdTick, &t.FinalKind, &t.FinalCredential, &t.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.Snapshot{}, ErrNotFound
	}
	if err != nil {
		return nil, task.Snapshot{}, err
	}
	sn, err := unmarshalSnapshot([]byte(snapshotJSON))
	if err != nil {
		return nil, task.Snapshot{}, err
	}
	return &t, sn, nil
}

// saveTaskTx writes the aggregate back, bumping the optimistic version. The
// final_kind and final_credential columns are left untouched here so they stay
// NULL until the single finalize barrier commits them.
func saveTaskTx(tx *sql.Tx, t *task.Task, sn task.Snapshot) error {
	snapshotJSON, err := marshalSnapshot(sn)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`UPDATE tasks SET intake_batch=?, generation=?, state=?, locked_snapshot_json=?,
		        rule_digest=?, version=version+1
		 WHERE task_id=?`,
		t.IntakeBatch, t.Generation, t.State, string(snapshotJSON),
		t.RuleDigest, string(t.ID))
	return err
}

// getPlotTx loads a catalogue plot inside tx.
func getPlotTx(tx *sql.Tx, id catalog.PlotID) (catalog.Plot, error) {
	var p catalog.Plot
	var start, end string
	err := tx.QueryRow(
		`SELECT plot_id, cultivar_id, harvest_start, harvest_end, rule_digest
		 FROM catalog_plots WHERE plot_id=?`, string(id)).
		Scan(&p.ID, &p.CultivarID, &start, &end, &p.RuleDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: plot %s", ErrNotFound, id)
	}
	if err != nil {
		return p, err
	}
	p.HarvestPeriod.Start, _ = time.Parse(time.RFC3339, start)
	p.HarvestPeriod.End, _ = time.Parse(time.RFC3339, end)
	return p, nil
}

// getRuleTx loads a catalogue rule by digest inside tx.
func getRuleTx(tx *sql.Tx, d catalog.RuleDigest) (catalog.Rule, error) {
	var r catalog.Rule
	var grades, thresholds, resources, points, reviewers string
	err := tx.QueryRow(
		`SELECT rule_id, digest, color_grades, thresholds_json, resources_json,
		        screening_points, reviewer_ids
		 FROM catalog_rules WHERE digest=?`, string(d)).
		Scan(&r.ID, &r.Digest, &grades, &thresholds, &resources, &points, &reviewers)
	if errors.Is(err, sql.ErrNoRows) {
		return r, catalog.ErrStaleRuleDigest
	}
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(grades), &r.ColorGrades)
	_ = json.Unmarshal([]byte(thresholds), &r.Thresholds)
	_ = json.Unmarshal([]byte(resources), &r.Resources)
	_ = json.Unmarshal([]byte(points), &r.ScreeningPoints)
	_ = json.Unmarshal([]byte(reviewers), &r.ReviewerIDs)
	return r, nil
}

// insertAudit records one audit event inside tx.
func insertAudit(tx *sql.Tx, eventID, taskID string, gen task.Generation, actor, eventType, subject, reason string, tick int64) error {
	_, err := tx.Exec(
		`INSERT INTO audit_events(event_id, task_id, generation, actor_id, event_type, subject_key, reason_code, created_tick)
		 VALUES(?,?,?,?,?,?,?,?)`,
		eventID, taskID, gen, actor, eventType, subject, reason, tick)
	return err
}

// requestDigest hashes a request payload to a deterministic hex string for
// idempotency comparison.
func requestDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(task.DigestPayload(string(b))), nil
}

// checkIdempotency returns the cached response body and a found flag. A replay
// with a different digest returns errOperationConflict; a matching replay
// returns the original serialized response.
func checkIdempotency(tx *sql.Tx, scope, opNo, digest string) (string, bool, error) {
	var reqDigest, body string
	err := tx.QueryRow(
		`SELECT request_digest, response_body_json FROM idempotency_keys
		 WHERE scope=? AND operation_no=?`, scope, opNo).
		Scan(&reqDigest, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if reqDigest != digest {
		return "", true, errOperationConflict
	}
	return body, true, nil
}

// recordIdempotency stores the response of a successful mutation.
func recordIdempotency(tx *sql.Tx, scope, opNo, digest string, code int, body string, tick int64) error {
	_, err := tx.Exec(
		`INSERT INTO idempotency_keys(scope, operation_no, request_digest, response_code, response_body_json, created_tick)
		 VALUES(?,?,?,?,?,?)`, scope, opNo, digest, code, body, tick)
	return err
}

// resolveGeneration normalizes an optional request generation against the
// current task generation. Zero (omitted) means "current".
func resolveGeneration(reqGen int64, current task.Generation) task.Generation {
	if reqGen == 0 {
		return current
	}
	return task.Generation(reqGen)
}

// sortedStrings returns a sorted, deduplicated copy of s.
func sortedStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	for _, v := range s {
		seen[v] = true
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
