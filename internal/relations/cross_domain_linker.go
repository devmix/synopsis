// Package relations provides cross-domain entity linking functionality.
package relations

import "context"

// EntityCandidate represents an entity being evaluated for cross-domain linking.
type EntityCandidate struct {
	ID          int
	Name        string
	Type        string
	Domain      string
	Description string
	TopChunks   []string
}

// LinkDecision is the structured result of an entity linking evaluation.
type LinkDecision struct {
	SameEntity bool    `json:"same_entity"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
}

// CrossDomainLinker defines the interface for cross-domain entity linking strategies.
type CrossDomainLinker interface {
	// Name returns the identifier for this linker implementation.
	Name() string

	// LinkPair evaluates whether two entities refer to the same real-world entity.
	LinkPair(ctx context.Context, a, b EntityCandidate) (*LinkDecision, error)
}
