-- Migration 000001: Create core pipeline tables
-- Tables: bchad_runs, bchad_stages, bchad_artifacts

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Pipeline runs: one row per feature spec submitted
CREATE TABLE bchad_runs (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id      TEXT         NOT NULL,
    pattern         TEXT         NOT NULL,
    spec_json       JSONB        NOT NULL,
    plan_json       JSONB,
    status          TEXT         NOT NULL DEFAULT 'pending',
    projected_cost  NUMERIC(10,4),
    actual_cost     NUMERIC(10,4) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

-- Pipeline stages: one row per stage per run
CREATE TABLE bchad_stages (
    id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id              UUID        NOT NULL REFERENCES bchad_runs(id) ON DELETE CASCADE,
    stage_type          TEXT        NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'pending',
    model               TEXT,
    attempt_count       INTEGER     NOT NULL DEFAULT 0,
    input_artifact_ids  UUID[],
    output_artifact_id  UUID,
    cost                NUMERIC(10,4) NOT NULL DEFAULT 0,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ
);

-- Stage artifacts: output blobs stored in S3, referenced here
CREATE TABLE bchad_artifacts (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    stage_id        UUID        NOT NULL REFERENCES bchad_stages(id) ON DELETE CASCADE,
    artifact_type   TEXT        NOT NULL,
    content_hash    TEXT        NOT NULL,
    s3_path         TEXT        NOT NULL,
    token_count     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_runs_product_status   ON bchad_runs(product_id, status);
CREATE INDEX idx_runs_created_at       ON bchad_runs(created_at DESC);
CREATE INDEX idx_stages_run_id         ON bchad_stages(run_id);
CREATE INDEX idx_stages_run_type       ON bchad_stages(run_id, stage_type);
CREATE INDEX idx_artifacts_stage_id    ON bchad_artifacts(stage_id);
