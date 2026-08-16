package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGlobalXML(t *testing.T, ontologyDir, content string) {
	t.Helper()
	if err := os.MkdirAll(ontologyDir, 0o755); err != nil {
		t.Fatalf("mkdir ontology dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ontologyDir, "global.xml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write global.xml: %v", err)
	}
}

func TestLoadGlobalPool(t *testing.T) {
	tests := []struct {
		name         string
		ontologyDir  string
		writeXML     bool
		xmlContent   string
		wantEntities int
		wantRel      int
		wantErr      bool
	}{
		{
			name:        "missing ontology dir yields empty pool",
			ontologyDir: filepath.Join(t.TempDir(), "nonexistent"),
			writeXML:    false,
		},
		{
			name:        "missing global.xml yields empty pool",
			ontologyDir: t.TempDir(),
			writeXML:    false,
		},
		{
			name:        "absent blocks yield empty pool",
			ontologyDir: t.TempDir(),
			writeXML:    true,
			xmlContent:  `<global><sources></sources></global>`,
		},
		{
			name:         "entities and relations parsed",
			ontologyDir:  t.TempDir(),
			writeXML:     true,
			xmlContent:   `<global><entities><entity id="e1" name="E1"><synonym>s</synonym></entity></entities><relations><relation predicate="r1" name="r1" source="e1" target="e1"/></relations></global>`,
			wantEntities: 1,
			wantRel:      1,
		},
		{
			name:        "malformed xml fails",
			ontologyDir: t.TempDir(),
			writeXML:    true,
			xmlContent:  `<global><entities></global>`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.writeXML {
				writeGlobalXML(t, tt.ontologyDir, tt.xmlContent)
			}
			pool, err := LoadGlobalPool(tt.ontologyDir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadGlobalPool() error = %v", err)
			}
			if pool == nil {
				t.Fatal("expected non-nil pool")
			}
			if len(pool.Entities) != tt.wantEntities {
				t.Errorf("entities = %d, want %d", len(pool.Entities), tt.wantEntities)
			}
			if len(pool.Relations) != tt.wantRel {
				t.Errorf("relations = %d, want %d", len(pool.Relations), tt.wantRel)
			}
		})
	}
}

func TestGlobalPoolValidate(t *testing.T) {
	tests := []struct {
		name    string
		pool    *GlobalXml
		wantErr string
	}{
		{
			name: "empty pool valid",
			pool: &GlobalXml{},
		},
		{
			name:    "missing entity Predicate",
			pool:    &GlobalXml{Entities: []EntityDef{{Name: "E"}}},
			wantErr: "entity Predicate is required",
		},
		{
			name:    "duplicate entity Predicate",
			pool:    &GlobalXml{Entities: []EntityDef{{ID: "e"}, {ID: "e"}}},
			wantErr: "duplicate global entity Predicate: e",
		},
		{
			name: "duplicate attribute name in entity",
			pool: &GlobalXml{Entities: []EntityDef{{
				ID:         "e",
				Attributes: []AttributeDef{{Name: "a"}, {Name: "a"}},
			}}},
			wantErr: "duplicate attribute name a",
		},
		{
			name: "ref attribute without target",
			pool: &GlobalXml{Entities: []EntityDef{{
				ID:         "e",
				Attributes: []AttributeDef{{Name: "ref", Type: "ref"}},
			}}},
			wantErr: "has type 'ref' but no target",
		},
		{
			name:    "duplicate relation Predicate",
			pool:    &GlobalXml{Entities: []EntityDef{{ID: "e"}}, Relations: []RelationDef{{Predicate: "r", Source: "e", Target: "e"}, {Predicate: "r", Source: "e", Target: "e"}}},
			wantErr: "duplicate global relation Predicate: r",
		},
		{
			name:    "relation references non-existent source",
			pool:    &GlobalXml{Entities: []EntityDef{{ID: "e"}}, Relations: []RelationDef{{Predicate: "r", Source: "missing", Target: "e"}}},
			wantErr: "non-existent source entity missing",
		},
		{
			name:    "relation references non-existent target",
			pool:    &GlobalXml{Entities: []EntityDef{{ID: "e"}}, Relations: []RelationDef{{Predicate: "r", Source: "e", Target: "missing"}}},
			wantErr: "non-existent target entity missing",
		},
		{
			name: "valid pool",
			pool: &GlobalXml{
				Entities:  []EntityDef{{ID: "e1"}, {ID: "e2"}},
				Relations: []RelationDef{{Predicate: "r", Source: "e1", Target: "e2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pool.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGlobalPoolMergeInto(t *testing.T) {
	pool := &GlobalXml{
		Entities: []EntityDef{
			{ID: "employee", Name: "Employee", Synonyms: []string{"сотрудник"}},
			{ID: "company", Name: "Company"},
		},
		Relations: []RelationDef{
			{Predicate: "works_at", Source: "employee", Target: "company"},
			{Predicate: "owns_policy", Source: "employee", Target: "employee"},
		},
	}

	tests := []struct {
		name      string
		cfg       *DomainConfig
		wantIDs   []string
		wantRel   []string
		wantWarns int
	}{
		{
			name: "merge identity with empty pool",
			cfg: &DomainConfig{
				Name:     "hr",
				Entities: []EntityDef{{ID: "grade"}},
			},
			wantIDs: []string{"grade"},
		},
		{
			name: "global-only entity appears in domain",
			cfg: &DomainConfig{
				Name: "it",
			},
			wantIDs: []string{"employee", "company"},
			wantRel: []string{"works_at", "owns_policy"},
		},
		{
			name: "domain override wins with warning",
			cfg: &DomainConfig{
				Name:     "hr",
				Entities: []EntityDef{{ID: "employee", Description: "domain version"}, {ID: "grade"}},
				Relations: []RelationDef{
					{Predicate: "works_at", Source: "employee", Target: "employee"},
				},
			},
			wantIDs:   []string{"employee", "grade", "company"},
			wantRel:   []string{"works_at", "owns_policy"},
			wantWarns: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p *GlobalXml
			if tt.name == "merge identity with empty pool" {
				p = &GlobalXml{}
			} else {
				p = pool
			}
			warnings := p.MergeInto(tt.cfg)
			if len(warnings) != tt.wantWarns {
				t.Errorf("warnings = %d, want %d (%v)", len(warnings), tt.wantWarns, warnings)
			}
			for _, id := range tt.wantIDs {
				if tt.cfg.GetEntityByID(id) == nil {
					t.Errorf("entity %q missing from merged config", id)
				}
			}
			for _, id := range tt.wantRel {
				if tt.cfg.GetRelationByID(id) == nil {
					t.Errorf("relation %q missing from merged config", id)
				}
			}
			// Overridden definition must keep the domain version verbatim.
			if e := tt.cfg.GetEntityByID("employee"); e != nil && tt.wantWarns > 0 {
				if e.Description != "domain version" {
					t.Errorf("override did not keep domain version: description = %q", e.Description)
				}
			}
		})
	}
}

func TestDiscoveryMergesGlobalPool(t *testing.T) {
	ontologyDir := t.TempDir()
	writeGlobalXML(t, ontologyDir, `<global>
	  <entities>
	    <entity id="employee" name="Employee"><synonym>сотрудник</synonym></entity>
	  </entities>
	  <relations>
	    <relation predicate="works_at" name="works_at" source="employee" target="employee"/>
	  </relations>
	</global>`)

	domainsDir := filepath.Join(ontologyDir, "domains")
	if err := os.MkdirAll(domainsDir, 0o755); err != nil {
		t.Fatalf("mkdir domains: %v", err)
	}
	// Domain defines a relation referencing a global-only entity; must pass validation after merge.
	domainXML := `<domain name="hr" version="1.0.0">
	  <entity id="grade" name="Grade"/>
	  <relation predicate="graded_for" name="graded_for" source="employee" target="grade"/>
	</domain>`
	if err := os.WriteFile(filepath.Join(domainsDir, "domain_hr.xml"), []byte(domainXML), 0o644); err != nil {
		t.Fatalf("write domain: %v", err)
	}

	registry, err := Discovery(ontologyDir)
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	hr, err := registry.Get("hr")
	if err != nil {
		t.Fatalf("Get(hr): %v", err)
	}
	if hr.GetEntityByID("employee") == nil {
		t.Error("global entity employee missing from merged hr config")
	}
	if hr.GetRelationByID("graded_for") == nil {
		t.Error("domain relation referencing global entity not present")
	}
	if err := hr.Validate(); err != nil {
		t.Errorf("merged config validation failed: %v", err)
	}
}

func TestDiscoveryNoGlobalXML(t *testing.T) {
	ontologyDir := t.TempDir()
	domainsDir := filepath.Join(ontologyDir, "domains")
	if err := os.MkdirAll(domainsDir, 0o755); err != nil {
		t.Fatalf("mkdir domains: %v", err)
	}
	domainXML := `<domain name="hr" version="1.0.0">
	  <entity id="employee" name="Employee"/>
	  <relation predicate="works_at" name="works_at" source="employee" target="employee"/>
	</domain>`
	if err := os.WriteFile(filepath.Join(domainsDir, "domain_hr.xml"), []byte(domainXML), 0o644); err != nil {
		t.Fatalf("write domain: %v", err)
	}

	registry, err := Discovery(ontologyDir)
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	hr, err := registry.Get("hr")
	if err != nil {
		t.Fatalf("Get(hr): %v", err)
	}
	types := hr.GetEntityTypes()
	if len(types) != 1 || types[0] != "employee" {
		t.Errorf("entity types = %v, want [employee]", types)
	}
}
