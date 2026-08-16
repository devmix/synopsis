package ner

import (
	"encoding/json"
	"strings"

	"github.com/devmix/synopsis/internal/domain"
)

func GenerateJSONSchema(cfg *domain.DomainConfig, useSchema bool) string {
	if cfg == nil || !useSchema {
		return ""
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{"entities"},
	}

	// Add entities schema
	entitySchema := GenerateEntitySchema(cfg)
	schema["properties"].(map[string]interface{})["entities"] = entitySchema

	// Add relations schema if relations are defined
	if len(cfg.Relations) > 0 {
		relationSchema := GenerateRelationSchema(cfg)
		schema["properties"].(map[string]interface{})["relations"] = relationSchema
	}

	// Convert to JSON string
	jsonBytes, err := json.MarshalIndent(schema, "", "    ")
	if err != nil {
		return ""
	}

	return string(jsonBytes)
}

func GenerateEntitySchema(cfg *domain.DomainConfig) map[string]interface{} {
	requiredFields := []interface{}{"name", "type"}

	schema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string"},
				"type":        map[string]interface{}{"type": "string"},
				"confidence":  map[string]interface{}{"type": "number"},
				"description": map[string]interface{}{"type": "string"},
				"attributes":  map[string]interface{}{"type": "object"},
			},
			"required": requiredFields,
		},
	}

	// If we have entity types, add enum constraint
	if len(cfg.Entities) > 0 {
		entityTypes := make([]string, len(cfg.Entities))
		for i, entity := range cfg.Entities {
			entityTypes[i] = strings.ToLower(entity.ID)
		}
		schema["items"].(map[string]interface{})["properties"].(map[string]interface{})["type"].(map[string]interface{})["enum"] = entityTypes
	}

	// Build attributes schema based on entity definitions
	if len(cfg.Entities) > 0 {
		attributesSchema := map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}

		// Collect all unique attribute names across all entities
		allAttributes := make(map[string]string) // name -> type
		for _, entity := range cfg.Entities {
			for _, attr := range entity.Attributes {
				if _, exists := allAttributes[attr.Name]; !exists {
					allAttributes[attr.Name] = attr.Type
				}
			}
		}

		// Build attribute property schemas
		attrsProps := attributesSchema["properties"].(map[string]interface{})
		for attrName, attrType := range allAttributes {
			propSchema := map[string]interface{}{}
			switch attrType {
			case "string":
				propSchema["type"] = "string"
			case "number", "int", "float":
				propSchema["type"] = "number"
			case "date", "datetime":
				propSchema["type"] = "string"
			case "boolean":
				propSchema["type"] = "boolean"
			case "ref":
				propSchema["type"] = "string"
			default:
				propSchema["type"] = "string"
			}
			attrsProps[attrName] = propSchema
		}

		schema["items"].(map[string]interface{})["properties"].(map[string]interface{})["attributes"] = attributesSchema
	}

	return schema
}

func GenerateRelationSchema(cfg *domain.DomainConfig) map[string]interface{} {
	requiredFields := []interface{}{"subject_type", "subject_name", "predicate", "object_type", "object_name"}

	schema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subject_type": map[string]interface{}{"type": "string"},
				"subject_name": map[string]interface{}{"type": "string"},
				"predicate":    map[string]interface{}{"type": "string"},
				"object_type":  map[string]interface{}{"type": "string"},
				"object_name":  map[string]interface{}{"type": "string"},
				"attributes":   map[string]interface{}{"type": "object"},
			},
			"required": requiredFields,
		},
	}

	// If we have relation types, add enum constraint for predicate
	if len(cfg.Relations) > 0 {
		relationTypes := make([]string, len(cfg.Relations))
		for i, relation := range cfg.Relations {
			relationTypes[i] = strings.ToLower(relation.Predicate)
		}
		schema["items"].(map[string]interface{})["properties"].(map[string]interface{})["predicate"].(map[string]interface{})["enum"] = relationTypes
	}

	// Build relation attributes schema if relations have attributes
	if len(cfg.Relations) > 0 {
		attributesSchema := map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}

		// Collect all unique relation attribute names
		allAttributes := make(map[string]string)
		for _, relation := range cfg.Relations {
			for _, attr := range relation.Attributes {
				if _, exists := allAttributes[attr.Name]; !exists {
					allAttributes[attr.Name] = attr.Type
				}
			}
		}

		// Build attribute property schemas
		attrsProps := attributesSchema["properties"].(map[string]interface{})
		for attrName, attrType := range allAttributes {
			propSchema := map[string]interface{}{}
			switch attrType {
			case "string":
				propSchema["type"] = "string"
			case "number", "int", "float":
				propSchema["type"] = "number"
			case "date", "datetime":
				propSchema["type"] = "string"
			case "boolean":
				propSchema["type"] = "boolean"
			case "condition":
				propSchema["type"] = "string"
			default:
				propSchema["type"] = "string"
			}
			attrsProps[attrName] = propSchema
		}

		schema["items"].(map[string]interface{})["properties"].(map[string]interface{})["attributes"] = attributesSchema
	}

	return schema
}
