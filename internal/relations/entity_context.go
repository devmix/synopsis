package relations

import (
	"encoding/json"

	"github.com/devmix/synopsis/internal/database/dao"
)

// EntityContext provides access to entity fields in CEL expressions.
type EntityContext struct {
	dao.Entity
}

// Metadata extracts a string value from the entity's JSON metadata by key.
func (ec EntityContext) Metadata(key string) *string {
	if ec.MetadataJSON == nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(*ec.MetadataJSON), &m); err != nil {
		return nil
	}
	if v, ok := m[key].(string); ok {
		return &v
	}
	return nil
}
