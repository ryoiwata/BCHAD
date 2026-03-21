-- Rollback migration 000001: Drop core pipeline tables

DROP INDEX IF EXISTS idx_artifacts_stage_id;
DROP INDEX IF EXISTS idx_stages_run_type;
DROP INDEX IF EXISTS idx_stages_run_id;
DROP INDEX IF EXISTS idx_runs_created_at;
DROP INDEX IF EXISTS idx_runs_product_status;

DROP TABLE IF EXISTS bchad_artifacts;
DROP TABLE IF EXISTS bchad_stages;
DROP TABLE IF EXISTS bchad_runs;
