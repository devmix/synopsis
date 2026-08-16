-- Migration 004: Backfill extracted_at for fact_sources with zero/empty timestamps.
-- Uses last KB ingest time (max(updated_at) from documents or facts), fallback to NOW().

UPDATE fact_sources SET extracted_at = COALESCE(
    (SELECT MAX(d.updated_at) FROM documents d WHERE d.updated_at != '' AND d.updated_at IS NOT NULL),
    (SELECT MAX(f.updated_at) FROM facts f WHERE f.updated_at != '' AND f.updated_at IS NOT NULL),
    datetime('now')
)
WHERE extracted_at = '' OR extracted_at IS NULL;
