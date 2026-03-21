-- Migration 000004: Create trust scoring and metrics tables
-- Tables: bchad_trust_scores, bchad_metrics

-- Trust scores: per-engineer per-product, updated after each run
CREATE TABLE bchad_trust_scores (
    id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    engineer_id         TEXT        NOT NULL,
    product_id          TEXT        NOT NULL,
    score               NUMERIC(5,2) NOT NULL DEFAULT 0,
    phase               TEXT        NOT NULL DEFAULT 'supervised' CHECK (phase IN ('supervised', 'gated', 'monitored')),
    signal_weights_json JSONB       NOT NULL DEFAULT '{}',
    last_10_runs_json   JSONB       NOT NULL DEFAULT '[]',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (engineer_id, product_id)
);

-- Metrics: time-series measurements for pipeline runs and stages
CREATE TABLE bchad_metrics (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id          UUID        NOT NULL REFERENCES bchad_runs(id) ON DELETE CASCADE,
    stage_id        UUID        REFERENCES bchad_stages(id) ON DELETE SET NULL,
    metric_name     TEXT        NOT NULL,
    metric_value    NUMERIC     NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_trust_scores_engineer_product ON bchad_trust_scores(engineer_id, product_id);
CREATE INDEX idx_trust_scores_phase            ON bchad_trust_scores(phase);
CREATE INDEX idx_metrics_run_id                ON bchad_metrics(run_id);
CREATE INDEX idx_metrics_stage_id              ON bchad_metrics(stage_id);
CREATE INDEX idx_metrics_name_recorded         ON bchad_metrics(metric_name, recorded_at DESC);
