-- Migration 001: Complete schema for Knowledge Base RAG service.
-- Consolidated from original migrations 001-004, 006-008.
-- Columns from ALTER TABLE migrations (005 content_hash, 007 weight) included inline.

CREATE TABLE IF NOT EXISTS documents (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type    TEXT NOT NULL, -- 'json', 'markdown', 'steam', 'unstructured'
    original_path  TEXT NOT NULL,
    metadata_json  TEXT,
    domain         TEXT NOT NULL DEFAULT '', -- JSON array of domain strings, e.g. ["hr","policy"]
    content_hash   TEXT,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chunks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id       INTEGER NOT NULL,
    chunk_text   TEXT NOT NULL,
    sequence_num INTEGER NOT NULL,
    start_offset INTEGER,
    end_offset   INTEGER,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (doc_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS entities (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL, -- 'employee', 'department', 'policy', 'system'
    name        TEXT NOT NULL,
    domain      TEXT NOT NULL DEFAULT '', -- domain this entity belongs to
    description TEXT,
    confidence  FLOAT,
    metadata_json TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(type, name, domain)
);

CREATE TABLE IF NOT EXISTS chunk_entities (
    chunk_id  INTEGER NOT NULL,
    entity_id INTEGER NOT NULL,
    PRIMARY KEY (chunk_id, entity_id),
    FOREIGN KEY (chunk_id) REFERENCES chunks(id) ON DELETE CASCADE,
    FOREIGN KEY (entity_id) REFERENCES entities(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS facts (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_entity_id INTEGER REFERENCES entities(id),
    predicate         TEXT NOT NULL,
    object_entity_id  INTEGER REFERENCES entities(id),
    domain            TEXT NOT NULL DEFAULT '', -- domain this fact belongs to
    metadata          TEXT, -- JSON metadata (threshold_amount, condition, etc.)
    status            TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'approved', 'rejected')),
    valid_from        DATE,
    valid_to          DATE,
    weight            INTEGER NOT NULL DEFAULT 1,
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (subject_entity_id, object_entity_id, predicate)
);

CREATE TABLE IF NOT EXISTS fact_sources (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    fact_id      INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
    document_id  TEXT NOT NULL,
    quote        TEXT, -- exact quote from source
    extracted_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entity_sources (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id   INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    UNIQUE(entity_id, document_id)
);

CREATE TABLE IF NOT EXISTS entity_links (
    subject_entity_id INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    target_entity_id  INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    relation_type     TEXT NOT NULL DEFAULT 'same_entity',
    method            TEXT NOT NULL,
    confidence        FLOAT NOT NULL DEFAULT 1.0,
    evidence          TEXT,
    PRIMARY KEY (subject_entity_id, target_entity_id, relation_type),
    CHECK (subject_entity_id != target_entity_id)
);

CREATE INDEX IF NOT EXISTS idx_entity_links_subject ON entity_links(subject_entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_links_target ON entity_links(target_entity_id);

-- Performance indexes for core tables.
CREATE INDEX IF NOT EXISTS idx_documents_domain ON documents(domain);
CREATE INDEX IF NOT EXISTS idx_chunks_doc_id ON chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_chunks_seq ON chunks(doc_id, sequence_num);
CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
CREATE INDEX IF NOT EXISTS idx_entities_domain ON entities(domain);

-- Indexes for facts queries.
CREATE INDEX IF NOT EXISTS idx_facts_status ON facts(status);
CREATE INDEX IF NOT EXISTS idx_facts_predicate ON facts(predicate);
CREATE INDEX IF NOT EXISTS idx_facts_subject ON facts(subject_entity_id);
CREATE INDEX IF NOT EXISTS idx_facts_object ON facts(object_entity_id);
CREATE INDEX IF NOT EXISTS idx_facts_valid ON facts(valid_from, valid_to);
CREATE INDEX IF NOT EXISTS idx_facts_domain ON facts(domain);
CREATE INDEX IF NOT EXISTS idx_fact_sources_fact ON fact_sources(fact_id);

-- Indexes for entity sources.
CREATE INDEX IF NOT EXISTS idx_entity_sources_entity_id ON entity_sources(entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_sources_document_id ON entity_sources(document_id);

-- FTS5 virtual table for full-text search on chunks.
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    chunk_text,
    content='chunks',
    content_rowid='id'
);

-- Synchronization triggers to keep FTS index in sync with chunks table.
CREATE TRIGGER IF NOT EXISTS chunks_fts_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, chunk_text) VALUES (new.id, new.chunk_text);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, chunk_text) VALUES('delete', old.id, old.chunk_text);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, chunk_text) VALUES('delete', old.id, old.chunk_text);
    INSERT INTO chunks_fts(rowid, chunk_text) VALUES (new.id, new.chunk_text);
END;
