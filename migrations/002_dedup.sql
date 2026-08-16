-- Migration 002: Document deduplication data migration.
-- Removes duplicate rows before creating the unique index.
-- Keeps the row with the highest ID (most recently inserted) for each original_path.

DELETE FROM documents WHERE id NOT IN (
    SELECT MAX(id) FROM documents GROUP BY original_path
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_original_path ON documents(original_path);
