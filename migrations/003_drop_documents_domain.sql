-- Migration 003: Drop documents.domain column and its index.
-- The domain column was never reliably populated; metadata_json ($.domain) is the single source of truth.

DROP INDEX IF EXISTS idx_documents_domain;
ALTER TABLE documents DROP COLUMN domain;
