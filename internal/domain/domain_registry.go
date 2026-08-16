package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/devmix/synopsis/internal/logger"
)

// domainEntry holds both the XML file path and parsed config for a registered domain.
type domainEntry struct {
	path string
	cfg  *DomainConfig
}

// DomainRegistry manages multiple domain configurations with thread-safe access.
type DomainRegistry struct {
	mu      sync.RWMutex
	domains map[string]domainEntry // name -> entry (path + config)
}

// NewDomainRegistry creates a new empty domain registry.
func NewDomainRegistry() *DomainRegistry {
	return &DomainRegistry{
		domains: make(map[string]domainEntry),
	}
}

// Register adds a domain config to the registry with its XML file path.
// Returns error if a domain with the same name already exists.
func (r *DomainRegistry) Register(name string, _path string, cfg *DomainConfig) error {
	if cfg == nil {
		return fmt.Errorf("domain config cannot be nil")
	}
	if name == "" {
		return fmt.Errorf("domain name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.domains[name]; exists {
		return fmt.Errorf("domain %q already registered", name)
	}

	r.domains[name] = domainEntry{path: _path, cfg: cfg}
	return nil
}

// Get returns a domain config by name.
// Returns error if the domain does not exist.
func (r *DomainRegistry) Get(name string) (*DomainConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.domains[name]
	if !exists {
		return nil, fmt.Errorf("domain %q not found", name)
	}

	return entry.cfg, nil
}

// GetAll returns all domain configs.
// The returned map is a copy to prevent external modification.
func (r *DomainRegistry) GetAll() map[string]*DomainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*DomainConfig, len(r.domains))
	for k, entry := range r.domains {
		result[k] = entry.cfg
	}
	return result
}

// GetEntityTypes returns all entity type IDs per domain.
func (r *DomainRegistry) GetEntityTypes() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string, len(r.domains))
	for name, entry := range r.domains {
		result[name] = entry.cfg.GetEntityTypes()
	}
	return result
}

// GetRelationTypes returns all relation type IDs per domain.
func (r *DomainRegistry) GetRelationTypes() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string, len(r.domains))
	for name, entry := range r.domains {
		result[name] = entry.cfg.GetRelationTypes()
	}
	return result
}

// MergeEntityTypes returns a unified entity type list across all domains.
// Duplicates are removed.
func (r *DomainRegistry) MergeEntityTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entitySet := make(map[string]bool)
	for _, entry := range r.domains {
		for _, entityType := range entry.cfg.GetEntityTypes() {
			entitySet[entityType] = true
		}
	}

	result := make([]string, 0, len(entitySet))
	for entityType := range entitySet {
		result = append(result, entityType)
	}
	return result
}

// HasDomain checks if a domain exists in the registry.
func (r *DomainRegistry) HasDomain(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.domains[name]
	return exists
}

// Paths returns the XML file paths for the given domain names, in the same order.
// Unknown names are skipped (not included in the result).
func (r *DomainRegistry) Paths(names []string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0, len(names))
	for _, name := range names {
		if entry, ok := r.domains[name]; ok {
			result = append(result, entry.path)
		}
	}
	return result
}

// Validate checks that all registered domains are well-formed.
// Returns an error if any domain is invalid.
func (r *DomainRegistry) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, entry := range r.domains {
		if err := entry.cfg.Validate(); err != nil {
			return fmt.Errorf("domain %q validation failed: %w", name, err)
		}
	}
	return nil
}

// LoadDomainConfigFile is a standalone helper to load a domain XML config file.
// It returns the parsed *DomainConfig or an error if the file cannot be read or parsed.
func LoadDomainConfigFile(path string) (*DomainConfig, error) {
	// Ensure the path is absolute for consistent error messages.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path for %s: %w", path, err)
	}

	// Check if file exists.
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("domain config file not found: %s", absPath)
	}

	// Load the domain config.
	cfg, err := LoadDomainConfig(absPath)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Discovery scans ontologyDir/domains/*.xml, merges the global entity and
// relation pool from ontologyDir/global.xml into each domain, and registers
// the effective (merged) configurations.
func Discovery(ontologyDir string) (*DomainRegistry, error) {
	return DiscoveryWithLogger(ontologyDir, nil)
}

// DiscoveryWithLogger behaves like Discovery but reports global pool override
// warnings through log. A nil logger discards the warnings.
func DiscoveryWithLogger(ontologyDir string, log *logger.Logger) (*DomainRegistry, error) {
	registry := NewDomainRegistry()

	pool, err := LoadGlobalPool(ontologyDir)
	if err != nil {
		return nil, fmt.Errorf("load global pool: %w", err)
	}
	if err := pool.Validate(); err != nil {
		return nil, fmt.Errorf("validate global pool: %w", err)
	}

	domainDir := filepath.Join(ontologyDir, "domains")
	entries, err := os.ReadDir(domainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return nil, fmt.Errorf("read domain directory %s: %w", domainDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matched, err := filepath.Match("*.xml", entry.Name())
		if err != nil {
			return nil, fmt.Errorf("match pattern *.xml for %s: %w", entry.Name(), err)
		}
		if !matched {
			continue
		}

		path := filepath.Join(domainDir, entry.Name())
		cfg, err := LoadDomainConfigFile(path)
		if err != nil {
			return nil, fmt.Errorf("load domain file %s: %w", path, err)
		}

		for _, warning := range pool.MergeInto(cfg) {
			if log != nil {
				log.Warn("global definition overridden", "warning", warning)
			}
		}

		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("validate domain from %s: %w", path, err)
		}
		if err := registry.Register(cfg.Name, path, cfg); err != nil {
			return nil, fmt.Errorf("register domain %q: %w", cfg.Name, err)
		}
	}

	return registry, nil
}
