-- Rollback migration 000002: Drop verification tables

DROP INDEX IF EXISTS idx_error_log_resolved;
DROP INDEX IF EXISTS idx_error_log_category;
DROP INDEX IF EXISTS idx_error_log_stage_id;
DROP INDEX IF EXISTS idx_gate_results_stage_attempt;
DROP INDEX IF EXISTS idx_gate_results_stage_id;

DROP TABLE IF EXISTS bchad_error_log;
DROP TABLE IF EXISTS bchad_gate_results;
