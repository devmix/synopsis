package dao_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
)

func TestDocumentDAO_ListPaginated_DomainFromMetadata(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	// Create a document where domain is in metadata_json.
	metaJSON1 := `{"domain":["hr","engineering"]}`
	_, err = docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/hr-policy.md",
		MetadataJSON: &metaJSON1,
	})
	if err != nil {
		t.Fatalf("create document 1: %v", err)
	}

	// Create a second document with domain in metadata_json.
	metaJSON2 := `{"domain":["product"]}`
	_, err = docDAO.Create(ctx, dao.Document{
		SourceType:   "json",
		OriginalPath: "/data/product.json",
		MetadataJSON: &metaJSON2,
	})
	if err != nil {
		t.Fatalf("create document 2: %v", err)
	}

	// Create a third document with no domain at all.
	_, err = docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/README.md",
	})
	if err != nil {
		t.Fatalf("create document 3: %v", err)
	}

	tests := []struct {
		name       string
		domain     string
		sourceType string
		wantCount  int
	}{
		{
			name:      "filter by hr domain from metadata",
			domain:    "hr",
			wantCount: 1,
		},
		{
			name:      "filter by engineering domain from metadata",
			domain:    "engineering",
			wantCount: 1,
		},
		{
			name:      "filter by product domain from column",
			domain:    "product",
			wantCount: 1,
		},
		{
			name:      "filter by nonexistent domain",
			domain:    "finance",
			wantCount: 0,
		},
		{
			name:       "no filter returns all",
			domain:     "",
			sourceType: "",
			wantCount:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, total, err := docDAO.ListPaginated(ctx, 0, 10, tt.domain, tt.sourceType)
			if err != nil {
				t.Fatalf("ListPaginated() error = %v", err)
			}
			if total != tt.wantCount {
				t.Errorf("total count = %d, want %d", total, tt.wantCount)
			}
			if len(docs) != tt.wantCount {
				t.Errorf("returned docs = %d, want %d", len(docs), tt.wantCount)
			}
		})
	}
}

func TestDocumentDAO_ListPaginated_DomainFromMetadataMulti(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	// Document with domain in metadata_json.
	metaJSON1 := `{"domain":["hr","engineering"]}`
	_, err = docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/hr.md",
		MetadataJSON: &metaJSON1,
	})
	if err != nil {
		t.Fatalf("create document 1: %v", err)
	}

	// Filter by "engineering" — should match from metadata_json.
	docs, total, err := docDAO.ListPaginated(ctx, 0, 10, "engineering", "")
	if err != nil {
		t.Fatalf("ListPaginated() error = %v", err)
	}
	if total != 1 {
		t.Errorf("total count for engineering = %d, want 1 (from metadata_json)", total)
	}
	if len(docs) != 1 {
		t.Errorf("returned docs = %d, want 1", len(docs))
	}

	// Filter by "hr" — should also match from metadata_json.
	docs2, total2, err := docDAO.ListPaginated(ctx, 0, 10, "hr", "")
	if err != nil {
		t.Fatalf("ListPaginated() error = %v", err)
	}
	if total2 != 1 {
		t.Errorf("total count for hr = %d, want 1 (from metadata_json)", total2)
	}
	if len(docs2) != 1 {
		t.Errorf("returned docs = %d, want 1", len(docs2))
	}
}
