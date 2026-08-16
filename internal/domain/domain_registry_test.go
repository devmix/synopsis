package domain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTempDomainConfig creates a temporary domain XML file for testing.
func createTempDomainConfig(t *testing.T, name, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, name+".xml")

	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	require.NoError(t, err)

	return tmpFile
}

// validDomainXML is a minimal valid domain configuration.
const validDomainXML = `<domain name="test" version="1.0.0" description="Test domain">
    <entity id="person" name="Person" description="A person">
        <attribute name="name" type="string" required="true"/>
    </entity>
    <relation predicate="knows" name="knows" source="person" target="person" description="Person knows person"/>
    <confidence auto_publish_threshold="0.85" review_threshold="0.60" reject_threshold="0.40"/>
</domain>`

// TestNewDomainRegistry tests that a new registry is created empty.
func TestNewDomainRegistry(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()
	assert.NotNil(t, registry)
	assert.Empty(t, registry.GetAll())
}

// TestDomainRegistry_Register tests registering domains.
func TestDomainRegistry_Register(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name        string
		path        string
		cfg         *DomainConfig
		expectError bool
	}{
		"valid registration": {
			name: "test",
			path: "/some/path/domain.xml",
			cfg: &DomainConfig{
				Name:        "test",
				Version:     "1.0.0",
				Description: "Test domain",
				Entities:    []EntityDef{{ID: "person", Name: "Person"}},
			},
			expectError: false,
		},
		"nil config": {
			name:        "test",
			cfg:         nil,
			expectError: true,
		},
		"empty name": {
			name: "",
			cfg: &DomainConfig{
				Name:    "test",
				Version: "1.0.0",
			},
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewDomainRegistry()
			err := registry.Register(tt.name, tt.path, tt.cfg)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, registry.HasDomain(tt.name))
				// Verify path is stored.
				paths := registry.Paths([]string{tt.name})
				assert.Equal(t, []string{tt.path}, paths)
			}
		})
	}
}

// TestDomainRegistry_RegisterDuplicate tests that duplicate registration fails.
func TestDomainRegistry_RegisterDuplicate(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()
	cfg := &DomainConfig{
		Name:    "test",
		Version: "1.0.0",
	}

	err := registry.Register("test", "", cfg)
	require.NoError(t, err)

	err = registry.Register("test", "", cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

// TestDomainRegistry_Get tests retrieving domains.
func TestDomainRegistry_Get(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()
	cfg := &DomainConfig{
		Name:    "test",
		Version: "1.0.0",
	}

	err := registry.Register("test", "", cfg)
	require.NoError(t, err)

	// Get existing domain
	retrieved, err := registry.Get("test")
	require.NoError(t, err)
	assert.Equal(t, cfg, retrieved)

	// Get non-existing domain
	_, err = registry.Get("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDomainRegistry_GetAll tests retrieving all domains.
func TestDomainRegistry_GetAll(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	cfg1 := &DomainConfig{Name: "hr", Version: "1.0.0"}
	cfg2 := &DomainConfig{Name: "it", Version: "1.0.0"}

	err := registry.Register("hr", "", cfg1)
	require.NoError(t, err)
	err = registry.Register("it", "", cfg2)
	require.NoError(t, err)

	all := registry.GetAll()
	assert.Len(t, all, 2)
	assert.Contains(t, all, "hr")
	assert.Contains(t, all, "it")

	// Verify returned map is a copy
	all["new"] = &DomainConfig{}
	assert.False(t, registry.HasDomain("new"))
}

// TestDomainRegistry_HasDomain tests checking domain existence.
func TestDomainRegistry_HasDomain(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()
	cfg := &DomainConfig{Name: "test", Version: "1.0.0"}

	err := registry.Register("test", "", cfg)
	require.NoError(t, err)

	assert.True(t, registry.HasDomain("test"))
	assert.False(t, registry.HasDomain("nonexistent"))
	assert.False(t, registry.HasDomain(""))
}

// TestDomainRegistry_GetEntityTypes tests getting entity types per domain.
func TestDomainRegistry_GetEntityTypes(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	cfg := &DomainConfig{
		Name:    "test",
		Version: "1.0.0",
		Entities: []EntityDef{
			{ID: "person", Name: "Person"},
			{ID: "organization", Name: "Organization"},
		},
	}

	err := registry.Register("test", "", cfg)
	require.NoError(t, err)

	entityTypes := registry.GetEntityTypes()
	assert.Len(t, entityTypes, 1)
	assert.ElementsMatch(t, []string{"person", "organization"}, entityTypes["test"])
}

// TestDomainRegistry_GetRelationTypes tests getting relation types per domain.
func TestDomainRegistry_GetRelationTypes(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	cfg := &DomainConfig{
		Name:    "test",
		Version: "1.0.0",
		Relations: []RelationDef{
			{Predicate: "works_for"},
			{Predicate: "manages"},
		},
	}

	err := registry.Register("test", "", cfg)
	require.NoError(t, err)

	relationTypes := registry.GetRelationTypes()
	assert.Len(t, relationTypes, 1)
	assert.ElementsMatch(t, []string{"works_for", "manages"}, relationTypes["test"])
}

// TestDomainRegistry_MergeEntityTypes tests merging entity types across domains.
func TestDomainRegistry_MergeEntityTypes(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	cfg1 := &DomainConfig{
		Name:    "hr",
		Version: "1.0.0",
		Entities: []EntityDef{
			{ID: "person", Name: "Person"},
			{ID: "department", Name: "Department"},
		},
	}

	cfg2 := &DomainConfig{
		Name:    "it",
		Version: "1.0.0",
		Entities: []EntityDef{
			{ID: "person", Name: "Person"}, // duplicate
			{ID: "system", Name: "System"},
		},
	}

	err := registry.Register("hr", "", cfg1)
	require.NoError(t, err)
	err = registry.Register("it", "", cfg2)
	require.NoError(t, err)

	merged := registry.MergeEntityTypes()
	assert.ElementsMatch(t, []string{"person", "department", "system"}, merged)
	assert.Len(t, merged, 3) // unique count
}

// TestLoadDomainConfigFile_Success tests loading a domain config from file and registering it.
func TestLoadDomainConfigFile_Success(t *testing.T) {
	t.Parallel()

	tmpFile := createTempDomainConfig(t, "test", validDomainXML)

	cfg, err := LoadDomainConfigFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "1.0.0", cfg.Version)

	registry := NewDomainRegistry()
	err = registry.Register(cfg.Name, tmpFile, cfg)
	require.NoError(t, err)
	assert.True(t, registry.HasDomain("test"))
}

// TestLoadDomainConfigFileNotFound tests loading a non-existent file.
func TestLoadDomainConfigFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadDomainConfigFile("/nonexistent/path/domain.xml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "domain config file not found")
}

// TestDomainRegistry_Validate tests registry validation.
func TestDomainRegistry_Validate(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	validCfg := &DomainConfig{
		Name:    "test",
		Version: "1.0.0",
		Entities: []EntityDef{
			{ID: "person", Name: "Person", Attributes: []AttributeDef{{Name: "name", Type: "string", Required: true}}},
		},
		Relations: []RelationDef{
			{Predicate: "knows", Source: "person", Target: "person"},
		},
	}

	err := registry.Register("test", "", validCfg)
	require.NoError(t, err)

	err = registry.Validate()
	require.NoError(t, err)
}

// TestDomainRegistry_ValidateInvalid tests validation with invalid domain.
func TestDomainRegistry_ValidateInvalid(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	// Invalid: missing version
	invalidCfg := &DomainConfig{
		Name: "test",
	}

	err := registry.Register("test", "", invalidCfg)
	require.NoError(t, err)

	err = registry.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// TestLoadDomainConfigFile tests the standalone LoadDomainConfigFile function.
func TestLoadDomainConfigFile(t *testing.T) {
	t.Parallel()

	tmpFile := createTempDomainConfig(t, "test", validDomainXML)

	cfg, err := LoadDomainConfigFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "1.0.0", cfg.Version)
	assert.Equal(t, 1, len(cfg.Entities))
	assert.Equal(t, 1, len(cfg.Relations))
}

// TestRegisterFromFiles tests loading multiple domain configs from files and registering them.
func TestRegisterFromFiles(t *testing.T) {
	t.Parallel()

	// Create temp domain files
	hrXML := `<domain name="hr" version="1.0.0" description="HR domain">
    <entity id="person" name="Person" description="A person">
        <attribute name="name" type="string" required="true"/>
    </entity>
    <relation predicate="knows" name="knows" source="person" target="person" description="Person knows person"/>
    <confidence auto_publish_threshold="0.85" review_threshold="0.60" reject_threshold="0.40"/>
</domain>`
	itXML := `<domain name="it" version="1.0.0" description="IT domain">
    <entity id="system" name="System" description="A system">
        <attribute name="name" type="string" required="true"/>
    </entity>
    <relation predicate="depends_on" name="depends_on" source="system" target="system" description="System depends on system"/>
    <confidence auto_publish_threshold="0.85" review_threshold="0.60" reject_threshold="0.40"/>
</domain>`

	hrFile := createTempDomainConfig(t, "hr", hrXML)
	itFile := createTempDomainConfig(t, "it", itXML)

	registry := NewDomainRegistry()

	for name, path := range map[string]string{
		"hr": hrFile,
		"it": itFile,
	} {
		cfg, err := LoadDomainConfigFile(path)
		require.NoError(t, err)
		err = registry.Register(name, path, cfg)
		require.NoError(t, err)
	}

	assert.Len(t, registry.GetAll(), 2)
	assert.True(t, registry.HasDomain("hr"))
	assert.True(t, registry.HasDomain("it"))

	// Verify paths are stored.
	paths := registry.Paths([]string{"hr", "it"})
	assert.Equal(t, []string{hrFile, itFile}, paths)
}

// TestRegisterInvalidDomain tests registering with invalid domain config.
func TestRegisterInvalidDomain(t *testing.T) {
	t.Parallel()

	// Create invalid domain file (missing version)
	invalidXML := `<domain name="test" description="Invalid">
    <entity id="person" name="Person">
        <attribute name="name" type="string"/>
    </entity>
</domain>`

	tmpFile := createTempDomainConfig(t, "invalid", invalidXML)

	cfg, err := LoadDomainConfigFile(tmpFile)
	require.NoError(t, err)

	registry := NewDomainRegistry()
	err = registry.Register("test", tmpFile, cfg)
	require.NoError(t, err)

	// Validation should fail since version is missing.
	err = registry.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// TestLoadDomainConfigFile_MissingFile tests loading with missing config file.
func TestLoadDomainConfigFile_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadDomainConfigFile("/nonexistent/domain.xml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDiscovery tests domain discovery from a directory.
func TestDiscovery(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	domainDir := filepath.Join(tmpDir, "domains")
	require.NoError(t, os.MkdirAll(domainDir, 0o755))

	// Write two domain XML files
	hrXML := `<domain name="hr" version="1.0">
    <entity id="person" name="Person"><attribute name="name" type="string" required="true"/></entity>
    <confidence/>
</domain>`
	itXML := `<domain name="it" version="2.0">
    <entity id="system" name="System"><attribute name="name" type="string" required="true"/></entity>
    <confidence/>
</domain>`

	require.NoError(t, os.WriteFile(filepath.Join(domainDir, "hr.xml"), []byte(hrXML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(domainDir, "it.xml"), []byte(itXML), 0o644))

	registry, err := Discovery(tmpDir)
	require.NoError(t, err)
	assert.Len(t, registry.GetAll(), 2)
	assert.True(t, registry.HasDomain("hr"))
	assert.True(t, registry.HasDomain("it"))

	// Verify paths are stored and point to real files.
	paths := registry.Paths([]string{"hr", "it"})
	assert.Len(t, paths, 2)
	for _, p := range paths {
		assert.FileExists(t, p)
	}
}

// TestDiscoveryEmptyDir tests discovery with an empty domains directory.
func TestDiscoveryEmptyDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	domainDir := filepath.Join(tmpDir, "domains")
	require.NoError(t, os.MkdirAll(domainDir, 0o755))

	registry, err := Discovery(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, registry.GetAll())
}

// TestDiscoveryMissingDir tests discovery when the domains directory doesn't exist.
func TestDiscoveryMissingDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	registry, err := Discovery(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, registry.GetAll())
}

// TestDomainRegistry_Concurrency tests thread-safe concurrent access.
func TestDomainRegistry_Concurrency(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	// Run concurrent operations
	done := make(chan bool)

	// Writers
	go func() {
		for i := 0; i < 100; i++ {
			cfg := &DomainConfig{Name: "test", Version: "1.0.0"}
			_ = registry.Register("test", "", cfg) // will fail after first, that's ok
		}
		done <- true
	}()

	// Readers
	go func() {
		for i := 0; i < 100; i++ {
			_ = registry.GetAll()
			_, _ = registry.Get("test")
			_ = registry.HasDomain("test")
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// No race conditions detected
}

// TestDomainRegistry_Paths tests the Paths method.
func TestDomainRegistry_Paths(t *testing.T) {
	t.Parallel()

	registry := NewDomainRegistry()

	cfg1 := &DomainConfig{Name: "hr", Version: "1.0.0"}
	cfg2 := &DomainConfig{Name: "it", Version: "1.0.0"}

	require.NoError(t, registry.Register("hr", "/path/to/hr.xml", cfg1))
	require.NoError(t, registry.Register("it", "/path/to/it.xml", cfg2))

	// Paths in order
	assert.Equal(t, []string{"/path/to/hr.xml", "/path/to/it.xml"}, registry.Paths([]string{"hr", "it"}))

	// Reversed order
	assert.Equal(t, []string{"/path/to/it.xml", "/path/to/hr.xml"}, registry.Paths([]string{"it", "hr"}))

	// Skip unknown names
	assert.Equal(t, []string{"/path/to/hr.xml"}, registry.Paths([]string{"hr", "unknown", "nonexistent"}))

	// All unknown
	assert.Empty(t, registry.Paths([]string{"unknown1", "unknown2"}))

	// Empty input
	assert.Empty(t, registry.Paths(nil))
	assert.Empty(t, registry.Paths([]string{}))
}
