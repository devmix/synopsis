-- Migration 005: General-purpose application key-value store.
-- Used for storing timestamps (e.g., last_linking_run) and other app-level state.

CREATE TABLE IF NOT EXISTS app_kv (
    key        TEXT PRIMARY KEY,
    value      TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
