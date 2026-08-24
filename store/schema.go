package store

// schema is the full SQLite DDL for the OlivePress persistence layer. It is
// applied idempotently on startup; WAL mode is enabled so concurrent writers
// serialize through SQLite's write lock while readers stay isolated.
const schema = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS catalog_plots (
    plot_id       TEXT PRIMARY KEY,
    cultivar_id   TEXT NOT NULL,
    harvest_start TEXT NOT NULL,
    harvest_end   TEXT NOT NULL,
    rule_digest   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS catalog_rules (
    rule_id          TEXT PRIMARY KEY,
    digest           TEXT NOT NULL UNIQUE,
    color_grades     TEXT NOT NULL,
    thresholds_json  TEXT NOT NULL,
    resources_json   TEXT NOT NULL,
    screening_points TEXT NOT NULL,
    reviewer_ids     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    task_id              TEXT PRIMARY KEY,
    intake_batch         TEXT NOT NULL,
    generation           INTEGER NOT NULL,
    state                TEXT NOT NULL,
    locked_snapshot_json TEXT NOT NULL,
    rule_digest          TEXT NOT NULL,
    created_at_tick      INTEGER NOT NULL,
    final_kind           TEXT,
    final_credential     TEXT,
    version              INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_intake_open
    ON tasks(intake_batch) WHERE final_kind IS NULL;

CREATE TABLE IF NOT EXISTS crate_gates (
    task_id        TEXT NOT NULL,
    crate_seal     TEXT NOT NULL,
    confirmed_by_a TEXT,
    confirmed_by_b TEXT,
    confirmed_tick INTEGER,
    PRIMARY KEY (task_id, crate_seal)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_crate_seal_open ON crate_gates(crate_seal);

CREATE TABLE IF NOT EXISTS blind_samples (
    task_id             TEXT NOT NULL,
    blind_code          TEXT NOT NULL,
    split_index         INTEGER NOT NULL,
    revealed_crate_seal TEXT,
    generation          INTEGER NOT NULL,
    PRIMARY KEY (task_id, blind_code)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blind_code_open ON blind_samples(blind_code);

CREATE TABLE IF NOT EXISTS resource_leases (
    task_id       TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    generation    INTEGER NOT NULL,
    start_tick    INTEGER NOT NULL,
    expire_tick   INTEGER NOT NULL,
    released_tick INTEGER,
    PRIMARY KEY (task_id, resource_type, resource_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_active_resource
    ON resource_leases(resource_type, resource_id) WHERE released_tick IS NULL;

CREATE TABLE IF NOT EXISTS maturity_cells (
    task_id     TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    crate_seal  TEXT NOT NULL,
    color_grade TEXT NOT NULL,
    count       INTEGER NOT NULL,
    version     INTEGER NOT NULL,
    PRIMARY KEY (task_id, crate_seal, color_grade)
);

CREATE TABLE IF NOT EXISTS evidence_versions (
    task_id       TEXT NOT NULL,
    generation    INTEGER NOT NULL,
    evidence_kind TEXT NOT NULL,
    subject_key   TEXT NOT NULL,
    version       INTEGER NOT NULL,
    fixed_value   INTEGER NOT NULL,
    unit_scale    INTEGER NOT NULL,
    raw_digest    TEXT NOT NULL,
    accepted      INTEGER NOT NULL,
    reason_code   TEXT,
    created_tick  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_evidence_lookup
    ON evidence_versions(task_id, generation, evidence_kind, subject_key);

CREATE TABLE IF NOT EXISTS adapter_calls (
    call_id       TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL,
    generation    INTEGER NOT NULL,
    adapter_kind  TEXT NOT NULL,
    target_key    TEXT NOT NULL,
    attempt_no    INTEGER NOT NULL,
    planned_tick  INTEGER NOT NULL,
    outcome       TEXT NOT NULL,
    payload_digest TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rechecks (
    task_id      TEXT NOT NULL,
    generation   INTEGER NOT NULL,
    reason       TEXT NOT NULL,
    crate_seals  TEXT NOT NULL,
    blind_codes  TEXT NOT NULL,
    test_holes   TEXT NOT NULL,
    created_tick INTEGER NOT NULL,
    PRIMARY KEY (task_id, generation)
);

CREATE TABLE IF NOT EXISTS reviews (
    task_id         TEXT NOT NULL,
    generation      INTEGER NOT NULL,
    reviewer_id     TEXT NOT NULL,
    role            TEXT NOT NULL,
    decision        TEXT NOT NULL,
    evidence_digest TEXT NOT NULL,
    created_tick    INTEGER NOT NULL,
    PRIMARY KEY (task_id, generation, reviewer_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_role
    ON reviews(task_id, generation, role);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope              TEXT NOT NULL,
    operation_no       TEXT NOT NULL,
    request_digest     TEXT NOT NULL,
    response_code      INTEGER NOT NULL,
    response_body_json TEXT NOT NULL,
    created_tick       INTEGER NOT NULL,
    PRIMARY KEY (scope, operation_no)
);

CREATE TABLE IF NOT EXISTS audit_events (
    event_id     TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL,
    generation   INTEGER NOT NULL,
    actor_id     TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    subject_key  TEXT,
    reason_code  TEXT,
    created_tick INTEGER NOT NULL
);
`
