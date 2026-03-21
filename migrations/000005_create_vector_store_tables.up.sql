-- Migration 000005: Enable pgvector and create codebase intelligence tables
-- Tables: bchad_code_patterns, bchad_file_structures, bchad_arch_decisions

CREATE EXTENSION IF NOT EXISTS vector;

-- Code patterns: annotated code examples extracted from merged PRs, embedded via Voyage Code 3
CREATE TABLE bchad_code_patterns (
    id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id          TEXT        NOT NULL,
    stage_type          TEXT        NOT NULL,
    language            TEXT        NOT NULL,
    entity_type         TEXT,
    has_permissions     BOOLEAN     NOT NULL DEFAULT FALSE,
    has_audit           BOOLEAN     NOT NULL DEFAULT FALSE,
    has_integrations    TEXT[]      NOT NULL DEFAULT '{}',
    pr_quality_score    NUMERIC(4,3) NOT NULL DEFAULT 0,
    content_text        TEXT        NOT NULL,
    metadata_json       JSONB       NOT NULL DEFAULT '{}',
    embedding           vector(1024),
    last_updated        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- File structures: embeddings of repository structural patterns
CREATE TABLE bchad_file_structures (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id      TEXT        NOT NULL,
    path_pattern    TEXT        NOT NULL,
    description     TEXT        NOT NULL,
    metadata_json   JSONB       NOT NULL DEFAULT '{}',
    embedding       vector(1024),
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Architecture decisions: embeddings of ADRs and architectural notes
CREATE TABLE bchad_arch_decisions (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id      TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    decision_text   TEXT        NOT NULL,
    metadata_json   JSONB       NOT NULL DEFAULT '{}',
    embedding       vector(1024),
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- HNSW vector indexes for approximate nearest-neighbor search (cosine distance)
CREATE INDEX idx_code_patterns_embedding
    ON bchad_code_patterns USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);

CREATE INDEX idx_file_structures_embedding
    ON bchad_file_structures USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);

CREATE INDEX idx_arch_decisions_embedding
    ON bchad_arch_decisions USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);

-- Composite B-tree indexes for filtered vector search (SQL WHERE + vector distance)
CREATE INDEX idx_code_patterns_product_stage
    ON bchad_code_patterns(product_id, stage_type);

CREATE INDEX idx_code_patterns_product_lang_entity
    ON bchad_code_patterns(product_id, language, entity_type);

CREATE INDEX idx_code_patterns_product_stage_lang
    ON bchad_code_patterns(product_id, stage_type, language);

CREATE INDEX idx_file_structures_product
    ON bchad_file_structures(product_id);

CREATE INDEX idx_arch_decisions_product
    ON bchad_arch_decisions(product_id);
