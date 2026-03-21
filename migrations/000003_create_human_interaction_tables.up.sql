-- Migration 000003: Create human interaction tables
-- Tables: bchad_approvals, bchad_prompt_log

-- Approval decisions: records every human approval or rejection
CREATE TABLE bchad_approvals (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    stage_id        UUID        NOT NULL REFERENCES bchad_stages(id) ON DELETE CASCADE,
    engineer_id     TEXT        NOT NULL,
    decision        TEXT        NOT NULL CHECK (decision IN ('approve', 'reject', 'pause')),
    guidance_note   TEXT,
    decided_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Prompt log: audit trail for every LLM call (hashes only, full content in S3)
CREATE TABLE bchad_prompt_log (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    stage_id        UUID        NOT NULL REFERENCES bchad_stages(id) ON DELETE CASCADE,
    attempt_number  INTEGER     NOT NULL,
    prompt_version  TEXT        NOT NULL,
    model           TEXT        NOT NULL,
    input_tokens    INTEGER     NOT NULL DEFAULT 0,
    output_tokens   INTEGER     NOT NULL DEFAULT 0,
    cost            NUMERIC(10,6) NOT NULL DEFAULT 0,
    prompt_hash     TEXT        NOT NULL,
    response_hash   TEXT        NOT NULL,
    latency_ms      INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_approvals_stage_id      ON bchad_approvals(stage_id);
CREATE INDEX idx_approvals_engineer_id   ON bchad_approvals(engineer_id);
CREATE INDEX idx_prompt_log_stage_id     ON bchad_prompt_log(stage_id);
CREATE INDEX idx_prompt_log_model        ON bchad_prompt_log(model);
CREATE INDEX idx_prompt_log_created_at   ON bchad_prompt_log(created_at DESC);
