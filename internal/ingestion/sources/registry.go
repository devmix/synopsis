package sources

import "fmt"

// Registry holds a map of source type names to their Source implementations.
type Registry struct {
	sources map[string]Source
}

// NewRegistry creates an empty registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]Source)}
}

// Register adds a source to the registry. Returns an error if the type is already registered.
func (r *Registry) Register(sourceType string, src Source) error {
	if _, exists := r.sources[sourceType]; exists {
		return fmt.Errorf("source type %q already registered", sourceType)
	}
	r.sources[sourceType] = src
	return nil
}

// Get returns the Source for the given type, or false if not found.
func (r *Registry) Get(sourceType string) (Source, bool) {
	s, ok := r.sources[sourceType]
	return s, ok
}

// Types returns all registered source type names.
func (r *Registry) Types() []string {
	types := make([]string, 0, len(r.sources))
	for t := range r.sources {
		types = append(types, t)
	}
	return types
}
