package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/ledger"
	"github.com/olivepress/fruit-intake-gate/task"

	_ "modernc.org/sqlite"
)

// SQLite is the durable Store backed by a SQLite WAL database. All mutating
// operations run in a single transaction; uniqueness is enforced by partial
// unique indexes so concurrent attempts produce one committed winner and
// deterministic losers.
type SQLite struct {
	db *sql.DB
}

// NewSQLite opens (creating if needed) a SQLite database at path, applies the
// schema, and enables WAL. Use ":memory:" for an ephemeral single-connection
// store in tests.
func NewSQLite(path string) (*SQLite, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection keeps :memory: databases stable and serializes
	// writers through SQLite's lock, which is exactly the single-writer model
	// the quality gate relies on.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

// Ping verifies the backend is reachable.
func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close releases the database connection.
func (s *SQLite) Close() error { return s.db.Close() }

// PutPlot persists a catalogue plot.
func (s *SQLite) PutPlot(ctx context.Context, p catalog.Plot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO catalog_plots(plot_id, cultivar_id, harvest_start, harvest_end, rule_digest)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(plot_id) DO UPDATE SET
		   cultivar_id=excluded.cultivar_id,
		   harvest_start=excluded.harvest_start,
		   harvest_end=excluded.harvest_end,
		   rule_digest=excluded.rule_digest`,
		string(p.ID), string(p.CultivarID),
		p.HarvestPeriod.Start.Format(time.RFC3339),
		p.HarvestPeriod.End.Format(time.RFC3339),
		string(p.RuleDigest))
	return err
}

// PutRule persists a catalogue rule.
func (s *SQLite) PutRule(ctx context.Context, r catalog.Rule) error {
	if r.Digest == "" {
		r.Digest = r.ComputeDigest()
	}
	grades, _ := json.Marshal(r.ColorGrades)
	thresholds, _ := json.Marshal(r.Thresholds)
	resources, _ := json.Marshal(r.Resources)
	points, _ := json.Marshal(r.ScreeningPoints)
	reviewers, _ := json.Marshal(r.ReviewerIDs)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO catalog_rules(rule_id, digest, color_grades, thresholds_json, resources_json, screening_points, reviewer_ids)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(rule_id) DO UPDATE SET
		   digest=excluded.digest, color_grades=excluded.color_grades,
		   thresholds_json=excluded.thresholds_json, resources_json=excluded.resources_json,
		   screening_points=excluded.screening_points, reviewer_ids=excluded.reviewer_ids`,
		r.ID, string(r.Digest), string(grades), string(thresholds), string(resources), string(points), string(reviewers))
	return err
}

// GetPlot loads a plot by id.
func (s *SQLite) GetPlot(ctx context.Context, id catalog.PlotID) (catalog.Plot, error) {
	var p catalog.Plot
	var start, end string
	err := s.db.QueryRowContext(ctx,
		`SELECT plot_id, cultivar_id, harvest_start, harvest_end, rule_digest FROM catalog_plots WHERE plot_id=?`,
		string(id)).Scan(&p.ID, &p.CultivarID, &start, &end, &p.RuleDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return p, mapErr(ErrNotFound)
	}
	if err != nil {
		return p, err
	}
	p.HarvestPeriod.Start, _ = time.Parse(time.RFC3339, start)
	p.HarvestPeriod.End, _ = time.Parse(time.RFC3339, end)
	return p, nil
}

// GetRule loads a rule by digest.
func (s *SQLite) GetRule(ctx context.Context, d catalog.RuleDigest) (catalog.Rule, error) {
	var r catalog.Rule
	var grades, thresholds, resources, points, reviewers string
	err := s.db.QueryRowContext(ctx,
		`SELECT rule_id, digest, color_grades, thresholds_json, resources_json, screening_points, reviewer_ids
		 FROM catalog_rules WHERE digest=?`, string(d)).
		Scan(&r.ID, &r.Digest, &grades, &thresholds, &resources, &points, &reviewers)
	if errors.Is(err, sql.ErrNoRows) {
		return r, mapErr(ErrNotFound)
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

// nextTick advances the persistent logical clock inside tx and returns the new
// tick. The write lock held by the transaction makes this race-free.
func nextTick(tx *sql.Tx) (int64, error) {
	var v int64
	err := tx.QueryRow(`SELECT value FROM meta WHERE key='clock'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES('clock', 1)`); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE meta SET value=value+1 WHERE key='clock'`); err != nil {
		return 0, err
	}
	return v + 1, nil
}

// mapErr converts a domain sentinel error into its stable coded error.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return err
	}
	switch {
	case errors.Is(err, catalog.ErrStaleRuleDigest):
		return NewCodedError(CodeStaleRuleDigest, "rule digest is stale")
	case errors.Is(err, catalog.ErrCultivarMismatch), errors.Is(err, catalog.ErrHarvestWindowMiss):
		return NewCodedError(CodePlotCultivarWindow, "plot/cultivar/harvest window mismatch")
	case errors.Is(err, catalog.ErrReviewerNotQualified):
		return NewCodedError(CodeReviewerNotQualified, "reviewer not qualified")
	case errors.Is(err, ledger.ErrResourceBusy):
		return NewCodedError(CodeResourceBusy, "resource busy")
	case errors.Is(err, task.ErrTerminalState):
		return NewCodedError(CodeTerminalState, "task is in a terminal state")
	case errors.Is(err, task.ErrInvalidTransition), errors.Is(err, errInvalidState):
		return NewCodedError(CodeInvalidState, "invalid task state")
	case errors.Is(err, ledger.ErrGenerationMismatch), errors.Is(err, arbiter.ErrGenerationMismatch):
		return NewCodedError(CodeGenerationMismatch, "generation mismatch")
	case errors.Is(err, evidence.ErrMissingColorGrade), errors.Is(err, evidence.ErrExtraColorGrade),
		errors.Is(err, evidence.ErrMissingCrate), errors.Is(err, evidence.ErrExtraCrate),
		errors.Is(err, evidence.ErrNegativeCount), errors.Is(err, evidence.ErrCountNotConserved):
		return NewCodedError(CodeCountNotConserved, "maturity count not conserved")
	case errors.Is(err, evidence.ErrFixedPointInvalid), errors.Is(err, evidence.ErrFixedPointSign),
		errors.Is(err, evidence.ErrFixedPointLength), errors.Is(err, evidence.ErrFixedPointScale):
		return NewCodedError(CodeFixedPointInvalid, "invalid fixed-point reading")
	case errors.Is(err, evidence.ErrFixedPointOverflow):
		return NewCodedError(CodeFixedPointOverflow, "fixed-point overflow")
	case errors.Is(err, evidence.ErrFixedPointDivideByZero):
		return NewCodedError(CodeFixedPointInvalid, "fixed-point divide by zero")
	case errors.Is(err, evidence.ErrAdapterFailed):
		return NewCodedError(CodeAdapterRetryPending, "adapter retry pending")
	case errors.Is(err, arbiter.ErrRejudgeConflict):
		return NewCodedError(CodeRejudgeGenerationConflict, "rejudge generation conflict")
	case errors.Is(err, arbiter.ErrRoleOverlap), errors.Is(err, arbiter.ErrReviewerReused):
		return NewCodedError(CodeRoleOverlap, "reviewer role overlap")
	case errors.Is(err, errDuplicateSeal):
		return NewCodedError(CodeDuplicateSeal, "duplicate crate seal")
	case errors.Is(err, errDuplicateBlindCode):
		return NewCodedError(CodeDuplicateBlindCode, "duplicate blind code")
	case errors.Is(err, errIntakeBatchDuplicate):
		return NewCodedError(CodeIntakeBatchDuplicate, "duplicate intake batch")
	case errors.Is(err, errOperationConflict):
		return NewCodedError(CodeOperationConflict, "operation number conflict")
	case errors.Is(err, ErrNotFound):
		return NewCodedError(CodeNotFound, "not found")
	default:
		return NewCodedError(CodeInvalidRequest, err.Error())
	}
}
