-- Rollback migration 000003: Drop human interaction tables

DROP INDEX IF EXISTS idx_prompt_log_created_at;
DROP INDEX IF EXISTS idx_prompt_log_model;
DROP INDEX IF EXISTS idx_prompt_log_stage_id;
DROP INDEX IF EXISTS idx_approvals_engineer_id;
DROP INDEX IF EXISTS idx_approvals_stage_id;

DROP TABLE IF EXISTS bchad_prompt_log;
DROP TABLE IF EXISTS bchad_approvals;
