-- Migration 000002: Create verification tables
-- Tables: bchad_gate_results, bchad_error_log

-- Verification gate results: one row per gate execution attempt
CREATE TABLE bchad_gate_results (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    stage_id        UUID        NOT NULL REFERENCES bchad_stages(id) ON DELETE CASCADE,
    attempt_number  INTEGER     NOT NULL,
    tier            INTEGER     NOT NULL CHECK (tier IN (1, 2)),
    passed          BOOLEAN     NOT NULL,
    checks_json     JSONB       NOT NULL DEFAULT '[]',
    error_output    TEXT,
    error_category  TEXT        NOT NULL DEFAULT '',
    duration_ms     INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Error log: records each classified error for audit and retry routing
CREATE TABLE bchad_error_log (
    id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    stage_id            UUID        NOT NULL REFERENCES bchad_stages(id) ON DELETE CASCADE,
    attempt_number      INTEGER     NOT NULL,
    category            TEXT        NOT NULL,
    raw_error           TEXT        NOT NULL,
    recovery_strategy   TEXT        NOT NULL,
    resolved            BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_gate_results_stage_id        ON bchad_gate_results(stage_id);
CREATE INDEX idx_gate_results_stage_attempt   ON bchad_gate_results(stage_id, attempt_number);
CREATE INDEX idx_error_log_stage_id           ON bchad_error_log(stage_id);
CREATE INDEX idx_error_log_category           ON bchad_error_log(category);
CREATE INDEX idx_error_log_resolved           ON bchad_error_log(resolved) WHERE resolved = FALSE;
