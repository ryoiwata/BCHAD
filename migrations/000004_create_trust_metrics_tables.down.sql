-- Rollback migration 000004: Drop trust and metrics tables

DROP INDEX IF EXISTS idx_metrics_name_recorded;
DROP INDEX IF EXISTS idx_metrics_stage_id;
DROP INDEX IF EXISTS idx_metrics_run_id;
DROP INDEX IF EXISTS idx_trust_scores_phase;
DROP INDEX IF EXISTS idx_trust_scores_engineer_product;

DROP TABLE IF EXISTS bchad_metrics;
DROP TABLE IF EXISTS bchad_trust_scores;
