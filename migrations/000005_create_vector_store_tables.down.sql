-- Rollback migration 000005: Drop vector store tables and indexes

DROP INDEX IF EXISTS idx_arch_decisions_product;
DROP INDEX IF EXISTS idx_file_structures_product;
DROP INDEX IF EXISTS idx_code_patterns_product_stage_lang;
DROP INDEX IF EXISTS idx_code_patterns_product_lang_entity;
DROP INDEX IF EXISTS idx_code_patterns_product_stage;
DROP INDEX IF EXISTS idx_arch_decisions_embedding;
DROP INDEX IF EXISTS idx_file_structures_embedding;
DROP INDEX IF EXISTS idx_code_patterns_embedding;

DROP TABLE IF EXISTS bchad_arch_decisions;
DROP TABLE IF EXISTS bchad_file_structures;
DROP TABLE IF EXISTS bchad_code_patterns;

-- Note: we do not drop the vector extension here as it may be used by other tables
-- DROP EXTENSION IF EXISTS vector;
