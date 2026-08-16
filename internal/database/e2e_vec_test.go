//go:build integration

package database_test

import (
	"os"
	"testing"

	"github.com/devmix/synopsis/internal/database"
)

func TestE2EVectorSearch(t *testing.T) {
	dbPath := "/tmp/e2e_vec_test.db"
	os.Remove(dbPath)

	// Open database with vector support
	db, err := database.Open(dbPath, 10) // Small dim for testing
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Verify sqlite-vec is available
	var vecVersion string
	err = db.DB().QueryRow("SELECT vec_version()").Scan(&vecVersion)
	if err != nil {
		t.Fatalf("sqlite-vec not available: %v", err)
	}
	t.Logf("sqlite-vec version: %s", vecVersion)

	// Create chunks_vec table manually (migrate would do this)
	_, err = db.DB().Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[10])`)
	if err != nil {
		t.Fatalf("Create chunks_vec: %v", err)
	}
	t.Log("✓ chunks_vec table created")

	// Insert test vector
	_, err = db.DB().Exec(`
		INSERT INTO chunks_vec (chunk_id, vector) VALUES (1, '[0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]')
	`)
	if err != nil {
		t.Fatalf("Insert vector: %v", err)
	}
	t.Log("✓ Vector inserted")

	// KNN search
	rows, err := db.DB().Query(`
		SELECT chunk_id, distance FROM chunks_vec 
		WHERE vector MATCH '[0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95, 1.05]' AND k = 1
	`)
	if err != nil {
		t.Fatalf("KNN search: %v", err)
	}
	defer rows.Close()

	var found bool
	for rows.Next() {
		var chunkID int
		var distance float64
		rows.Scan(&chunkID, &distance)
		t.Logf("✓ KNN result: chunk_id=%d, distance=%.6f", chunkID, distance)
		found = true
	}

	if !found {
		t.Error("Expected at least 1 KNN result")
	}
}
