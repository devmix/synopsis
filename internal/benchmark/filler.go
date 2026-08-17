package benchmark

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/devmix/synopsis/internal/database/dao"
)

// FillReport summarizes the result of filling the database with a dataset.
type FillReport struct {
	Duration time.Duration    `json:"duration"`
	Tables   map[string]int64 `json:"tables"` // inserted rows per table (including chunks_vec)
	Vectors  int              `json:"vectors"`
}

// Embedder generates embedding vectors for batches of texts and reports the
// vector dimension. The embedding.Provider interface satisfies this contract.
type Embedder interface {
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
	VectorDim() int
}

// FillOptions controls the fill process.
type FillOptions struct {
	// BatchSize is the number of texts per embedding call and per vector insert
	// transaction. Defaults to 100 when zero or negative.
	BatchSize int

	// Progress is an optional callback invoked after each embedding batch with
	// (embedded so far, total). It may be nil.
	Progress func(embedded, total int)
}

// ftsTriggerNames are the FTS5 sync triggers created by migrations/001_schema.sql.
// They are dropped for the duration of a bulk fill and restored afterwards.
var ftsTriggerNames = []string{"chunks_fts_ai", "chunks_fts_ad", "chunks_fts_au"}

const (
	ftsTableName = "chunks_fts"
	vecTableName = "chunks_vec"

	defaultEmbeddingBatchSize = 100
)

// Fill clears the knowledge base tables and populates them with the dataset.
//
// The procedure is:
//  1. drop FTS5 sync triggers (bulk DML must not maintain the index row by row),
//  2. clear all scalar tables and insert every row in one transaction,
//  3. rebuild the FTS index from chunks and restore the triggers,
//  4. recreate chunks_vec and insert real embeddings batch by batch.
func Fill(ctx context.Context, db *sql.DB, ds *Dataset, emb Embedder, opts FillOptions) (*FillReport, error) {
	start := time.Now()

	if err := dropFTSTriggers(db); err != nil {
		return nil, fmt.Errorf("drop fts triggers: %w", err)
	}
	defer func() { _ = restoreFTSTriggers(db) }() // safety net for error paths

	if n, err := fillScalarTables(ctx, db, ds); err != nil {
		return nil, fmt.Errorf("fill scalar tables: %w", err)
	} else if n == 0 && len(ds.Chunks) > 0 {
		return nil, fmt.Errorf("no chunks were inserted")
	}

	if err := rebuildFTSIndex(db); err != nil {
		return nil, fmt.Errorf("rebuild fts index: %w", err)
	}
	// Restore the sync triggers as soon as the FTS index is consistent so a
	// crash during (potentially long) vector embedding cannot leave them gone.
	if err := restoreFTSTriggers(db); err != nil {
		return nil, fmt.Errorf("restore fts triggers: %w", err)
	}

	if emb == nil {
		return nil, fmt.Errorf("an embedding provider is required to fill chunk vectors")
	}
	if err := fillVectors(ctx, db, ds, emb, opts); err != nil {
		return nil, fmt.Errorf("fill chunk vectors: %w", err)
	}

	report := &FillReport{Duration: time.Since(start)}
	if n, err := tableCounts(ctx, db); err != nil {
		return nil, fmt.Errorf("collect table counts: %w", err)
	} else {
		report.Tables = n
	}
	report.Vectors = len(ds.Chunks)

	return report, nil
}

// fillScalarTables clears and refills all non-vector tables in a single
// transaction. Deletions follow FK-safe order (children first). It returns the
// number of inserted chunks.
func fillScalarTables(ctx context.Context, db *sql.DB, ds *Dataset) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	cleanupSQLs := []string{
		"DELETE FROM fact_sources",
		"DELETE FROM chunk_entities",
		"DELETE FROM entity_sources",
		"DELETE FROM facts",
		"DELETE FROM entity_links",
		"DELETE FROM entities",
		"DELETE FROM chunks",
		"DELETE FROM documents",
	}
	for _, stmt := range cleanupSQLs {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return 0, fmt.Errorf("clear tables: %w", err)
		}
	}

	const insertDocument = `INSERT INTO documents (id, source_type, original_path, metadata_json, content_hash) VALUES (?, ?, ?, ?, ?)`
	stmtDoc, err := tx.PrepareContext(ctx, insertDocument)
	if err != nil {
		return 0, fmt.Errorf("prepare documents insert: %w", err)
	}
	for i := range ds.Documents {
		d := &ds.Documents[i]
		if _, err := stmtDoc.ExecContext(ctx, d.ID, d.SourceType, d.OriginalPath, d.MetadataJSON, d.ContentHash); err != nil {
			stmtDoc.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert document %d: %w", d.ID, err)
		}
	}
	if err := stmtDoc.Close(); err != nil {
		return 0, fmt.Errorf("close documents insert: %w", err)
	}

	const insertChunk = `INSERT INTO chunks (id, doc_id, chunk_text, sequence_num, start_offset, end_offset) VALUES (?, ?, ?, ?, ?, ?)`
	stmtChunk, err := tx.PrepareContext(ctx, insertChunk)
	if err != nil {
		return 0, fmt.Errorf("prepare chunks insert: %w", err)
	}
	for i := range ds.Chunks {
		c := &ds.Chunks[i]
		if _, err := stmtChunk.ExecContext(ctx, c.ID, c.DocID, c.Text, c.SeqNum, c.StartOffset, c.EndOffset); err != nil {
			stmtChunk.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert chunk %d: %w", c.ID, err)
		}
	}
	if err := stmtChunk.Close(); err != nil {
		return 0, fmt.Errorf("close chunks insert: %w", err)
	}

	const insertEntity = `INSERT INTO entities (id, type, name, domain, description, confidence) VALUES (?, ?, ?, ?, ?, ?)`
	stmtEnt, err := tx.PrepareContext(ctx, insertEntity)
	if err != nil {
		return 0, fmt.Errorf("prepare entities insert: %w", err)
	}
	for i := range ds.Entities {
		e := &ds.Entities[i]
		if _, err := stmtEnt.ExecContext(ctx, e.ID, e.Type, e.Name, e.Domain, e.Description, e.Confidence); err != nil {
			stmtEnt.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert entity %d: %w", e.ID, err)
		}
	}
	if err := stmtEnt.Close(); err != nil {
		return 0, fmt.Errorf("close entities insert: %w", err)
	}

	const insertChunkEntity = `INSERT OR IGNORE INTO chunk_entities (chunk_id, entity_id) VALUES (?, ?)`
	stmtCE, err := tx.PrepareContext(ctx, insertChunkEntity)
	if err != nil {
		return 0, fmt.Errorf("prepare chunk_entities insert: %w", err)
	}
	for i := range ds.ChunkEntities {
		ce := &ds.ChunkEntities[i]
		if _, err := stmtCE.ExecContext(ctx, ce.ChunkID, ce.EntityID); err != nil {
			stmtCE.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert chunk_entity (%d,%d): %w", ce.ChunkID, ce.EntityID, err)
		}
	}
	if err := stmtCE.Close(); err != nil {
		return 0, fmt.Errorf("close chunk_entities insert: %w", err)
	}

	const insertFact = `INSERT INTO facts (id, subject_entity_id, predicate, object_entity_id, domain, status, valid_from, valid_to, weight) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	stmtFact, err := tx.PrepareContext(ctx, insertFact)
	if err != nil {
		return 0, fmt.Errorf("prepare facts insert: %w", err)
	}
	for i := range ds.Facts {
		f := &ds.Facts[i]
		if _, err := stmtFact.ExecContext(ctx, f.ID, f.SubjectID, f.Predicate, f.ObjectID, f.Domain, f.Status, f.ValidFrom, f.ValidTo, f.Weight); err != nil {
			stmtFact.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert fact %d: %w", f.ID, err)
		}
	}
	if err := stmtFact.Close(); err != nil {
		return 0, fmt.Errorf("close facts insert: %w", err)
	}

	const insertFactSource = `INSERT INTO fact_sources (id, fact_id, document_id, quote) VALUES (?, ?, ?, ?)`
	stmtFS, err := tx.PrepareContext(ctx, insertFactSource)
	if err != nil {
		return 0, fmt.Errorf("prepare fact_sources insert: %w", err)
	}
	for i := range ds.FactSources {
		fs := &ds.FactSources[i]
		if _, err := stmtFS.ExecContext(ctx, fs.ID, fs.FactID, strconv.Itoa(fs.DocumentID), fs.Quote); err != nil {
			stmtFS.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert fact_source %d: %w", fs.ID, err)
		}
	}
	if err := stmtFS.Close(); err != nil {
		return 0, fmt.Errorf("close fact_sources insert: %w", err)
	}

	const insertEntitySource = `INSERT OR IGNORE INTO entity_sources (id, entity_id, document_id) VALUES (?, ?, ?)`
	stmtES, err := tx.PrepareContext(ctx, insertEntitySource)
	if err != nil {
		return 0, fmt.Errorf("prepare entity_sources insert: %w", err)
	}
	for i := range ds.EntitySources {
		es := &ds.EntitySources[i]
		if _, err := stmtES.ExecContext(ctx, es.ID, es.EntityID, es.DocumentID); err != nil {
			stmtES.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert entity_source %d: %w", es.ID, err)
		}
	}
	if err := stmtES.Close(); err != nil {
		return 0, fmt.Errorf("close entity_sources insert: %w", err)
	}

	const insertEntityLink = `INSERT OR IGNORE INTO entity_links (subject_entity_id, target_entity_id, relation_type, method, confidence, evidence) VALUES (?, ?, ?, ?, ?, ?)`
	stmtEL, err := tx.PrepareContext(ctx, insertEntityLink)
	if err != nil {
		return 0, fmt.Errorf("prepare entity_links insert: %w", err)
	}
	for i := range ds.EntityLinks {
		el := &ds.EntityLinks[i]
		if _, err := stmtEL.ExecContext(ctx, el.SubjectID, el.TargetID, el.RelationType, el.Method, el.Confidence, el.Evidence); err != nil {
			stmtEL.Close() //nolint:errcheck
			return 0, fmt.Errorf("insert entity_link (%d->%d): %w", el.SubjectID, el.TargetID, err)
		}
	}
	if err := stmtEL.Close(); err != nil {
		return 0, fmt.Errorf("close entity_links insert: %w", err)
	}

	// Pin AUTOINCREMENT sequences to the max explicit IDs so that future
	// ingestion inserts continue beyond this dataset regardless of scale changes.
	if err := resetAutoincrement(ctx, tx, "documents", len(ds.Documents)); err != nil {
		return 0, err
	}
	if err := resetAutoincrement(ctx, tx, "chunks", len(ds.Chunks)); err != nil {
		return 0, err
	}
	if err := resetAutoincrement(ctx, tx, "entities", len(ds.Entities)); err != nil {
		return 0, err
	}
	if err := resetAutoincrement(ctx, tx, "facts", len(ds.Facts)); err != nil {
		return 0, err
	}
	if err := resetAutoincrement(ctx, tx, "fact_sources", len(ds.FactSources)); err != nil {
		return 0, err
	}
	if err := resetAutoincrement(ctx, tx, "entity_sources", len(ds.EntitySources)); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit scalar fill: %w", err)
	}

	return len(ds.Chunks), nil
}

// resetAutoincrement resets the AUTOINCREMENT counter of table to maxID so that
// the next auto-assigned ID is maxID+1. A non-positive maxID clears the entry.
func resetAutoincrement(ctx context.Context, tx *sql.Tx, table string, maxID int) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM sqlite_sequence WHERE name = ?", table); err != nil {
		return fmt.Errorf("reset sequence %s: %w", table, err)
	}
	if maxID > 0 {
		if _, err := tx.ExecContext(ctx, "INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)", table, maxID); err != nil {
			return fmt.Errorf("set sequence %s to %d: %w", table, maxID, err)
		}
	}
	return nil
}

// resetVecTable drops chunks_vec and recreates it with the given dimension.
func resetVecTable(ctx context.Context, db *sql.DB, dim int) error {
	if dim <= 0 {
		return fmt.Errorf("vector dimension must be positive, got %d", dim)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+vecTableName); err != nil {
		return fmt.Errorf("drop vector table: %w", err)
	}
	createSQL := fmt.Sprintf(
		"CREATE VIRTUAL TABLE %s USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[%d])",
		vecTableName, dim)
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create vector table with dim %d: %w", dim, err)
	}
	return nil
}

// rebuildFTSIndex rebuilds the content-backed FTS5 index from the chunks table.
func rebuildFTSIndex(db *sql.DB) error {
	if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s(%s) VALUES('rebuild')", ftsTableName, ftsTableName)); err != nil {
		return fmt.Errorf("fts rebuild: %w", err)
	}
	return nil
}

// fillVectors recreates chunks_vec and inserts embeddings batch by batch so a
// large dataset never keeps an oversized write transaction open.
func fillVectors(ctx context.Context, db *sql.DB, ds *Dataset, emb Embedder, opts FillOptions) error {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbeddingBatchSize
	}

	if err := resetVecTable(ctx, db, emb.VectorDim()); err != nil {
		return fmt.Errorf("reset vector table: %w", err)
	}

	insertSQL := "INSERT OR REPLACE INTO chunks_vec (chunk_id, vector) VALUES (?, ?)"

	total := len(ds.Chunks)
	embedded := 0
	for startIdx := 0; startIdx < total; startIdx += batchSize {
		endIdx := startIdx + batchSize
		if endIdx > total {
			endIdx = total
		}
		batch := ds.Chunks[startIdx:endIdx]

		texts := make([]string, len(batch))
		for i := range batch {
			texts[i] = batch[i].Text
		}

		vectors, err := emb.GenerateEmbeddings(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed chunks %d..%d: %w", startIdx+1, endIdx, err)
		}
		if len(vectors) != len(batch) {
			return fmt.Errorf("embedding provider returned %d vectors for %d texts", len(vectors), len(batch))
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin vector batch tx: %w", err)
		}
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("prepare vector insert: %w", err)
		}
		for i := range batch {
			if _, err := stmt.ExecContext(ctx, batch[i].ID, dao.FormatVector(vectors[i])); err != nil {
				stmt.Close()  //nolint:errcheck
				tx.Rollback() //nolint:errcheck
				return fmt.Errorf("insert vector for chunk %d: %w", batch[i].ID, err)
			}
		}
		if err := stmt.Close(); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("close vector insert: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit vector batch: %w", err)
		}

		embedded += len(batch)
		if opts.Progress != nil {
			opts.Progress(embedded, total)
		}
	}

	return nil
}

// tableCounts returns row counts for all knowledge base tables.
func tableCounts(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	tables := []string{
		"documents", "chunks", "entities", "facts",
		"fact_sources", "entity_sources", "chunk_entities", "entity_links", vecTableName,
	}

	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var n int64
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		counts[table] = n
	}

	return counts, nil
}

// dropFTSTriggers removes the FTS5 sync triggers before bulk DML.
func dropFTSTriggers(db *sql.DB) error {
	for _, name := range ftsTriggerNames {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS " + name); err != nil {
			return fmt.Errorf("drop trigger %s: %w", name, err)
		}
	}
	return nil
}

// restoreFTSTriggers recreates the FTS5 sync triggers dropped before a bulk fill.
// The definitions mirror migrations/001_schema.sql and are idempotent.
func restoreFTSTriggers(db *sql.DB) error {
	const stmt = `
CREATE TRIGGER IF NOT EXISTS chunks_fts_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, chunk_text) VALUES (new.id, new.chunk_text);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, chunk_text) VALUES('delete', old.id, old.chunk_text);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, chunk_text) VALUES('delete', old.id, old.chunk_text);
    INSERT INTO chunks_fts(rowid, chunk_text) VALUES (new.id, new.chunk_text);
END;`
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("restore fts triggers: %w", err)
	}
	return nil
}
