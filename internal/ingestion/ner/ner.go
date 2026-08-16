// Package ner provides named entity recognition providers for extracting entities from text.
package ner

import (
	"context"
)

type Entity struct {
	Name        string                 // normalized entity name
	Type        string                 // entity type (e.g., "PERSON", "LOCATION")
	Description string                 // description derived from surrounding context
	Confidence  float64                // extraction confidence in [0.0, 1.0]
	Domain      string                 // domain this entity belongs to (from source config)
	Metadata    map[string]interface{} // additional provenance data
}

type Fact struct {
	SubjectType string
	SubjectName string
	Predicate   string
	ObjectType  string
	ObjectName  string
	Domain      string // domain this fact belongs to (from source config)
	Metadata    map[string]interface{}
}

type Result struct {
	Entities []Entity
	Facts    []Fact
}

type Provider interface {
	Name() string

	ExtractEntities(ctx context.Context, content string, metadata map[string]interface{}) (*Result, error)
}
