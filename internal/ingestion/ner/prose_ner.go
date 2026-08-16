// Package ner provides named entity recognition providers for extracting entities from text.
package ner

import (
	"context"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
	prose "github.com/tsawler/prose/v3"
)

// Default confidence thresholds
const (
	defaultMinConfidence         = 0.5
	defaultLocationMinConfidence = 0.9
)

// ProseNEROptions configures the prose-based NER provider using tsawler/prose v3.
type ProseNEROptions struct {
	Config config.ProseNERConfig
}

// ProseNER provides NER using the tsawler/prose v3 library.
// It leverages prose's built-in tokenization, POS tagging, and named-entity extraction
// for fast offline processing without external API dependencies.
type ProseNER struct {
	cfg ProseNEROptions
	log *logger.Logger // structured logger; required
}

// NewProseNER creates a new prose-based NER provider with the given configuration and logger (required).
func NewProseNER(opts ProseNEROptions, log *logger.Logger) *ProseNER {
	// Apply defaults
	if opts.Config.MinConfidence == 0 {
		opts.Config.MinConfidence = defaultMinConfidence
	}
	if opts.Config.LocationMinConfidence == 0 {
		opts.Config.LocationMinConfidence = defaultLocationMinConfidence
	}
	return &ProseNER{cfg: opts, log: log}
}

func (p *ProseNER) Name() string {
	return "prose"
}

func (p *ProseNER) ExtractEntities(ctx context.Context, content string, metadata map[string]interface{}) (*Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("prose ner: context cancelled: %w", ctx.Err())
	}

	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	docOpts := buildDocOpts(p.cfg)

	doc, err := prose.NewDocument(content, docOpts...)
	if err != nil {
		return nil, fmt.Errorf("prose ner: create document: %w", err)
	}

	nerEntities := extractProseEntities(doc, p.cfg)

	return &Result{Entities: nerEntities}, err
}

func (p *ProseNER) ExtractPOS(
	ctx context.Context, content string,
) ([]TokenInfo, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("prose ner: context cancelled: %w", ctx.Err())
	}

	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	docOpts := buildDocOpts(p.cfg)

	doc, err := prose.NewDocument(content, docOpts...)
	if err != nil {
		return nil, fmt.Errorf("prose ner: create document for POS: %w", err)
	}

	tokens := make([]TokenInfo, 0, len(doc.Tokens()))
	for _, tok := range doc.Tokens() {
		tokens = append(tokens, TokenInfo{
			Text:  tok.Text,
			Tag:   tok.Tag,
			Label: tok.Label,
		})
	}

	return tokens, nil
}

type TokenInfo struct {
	Text  string // the token text
	Tag   string // part-of-speech tag (e.g., "NNP", "VBZ")
	Label string // IOB entity label (e.g., "B-PERSON", "I-GPE", "O")
}

func buildDocOpts(opts ProseNEROptions) []prose.DocOpt {
	result := make([]prose.DocOpt, 0, 3)

	// Tokenization is always needed as base for other operations.
	result = append(result, prose.WithTokenization(true))

	if opts.Config.EnablePOS || opts.Config.EnableNER {
		// POS tagging is required for NER in prose pipeline.
		result = append(result, prose.WithTagging(true))
	}

	if opts.Config.EnableNER {
		result = append(result, prose.WithExtraction(true))
	} else {
		result = append(result, prose.WithExtraction(false))
	}

	return result
}

func extractProseEntities(doc *prose.Document, opts ProseNEROptions) []Entity {
	if doc == nil {
		return nil
	}

	proseEntities := doc.Entities()
	if len(proseEntities) == 0 {
		return nil
	}

	// Build type filter set if configured.
	var typeSet map[string]bool
	if len(opts.Config.EntityTypes) > 0 {
		typeSet = make(map[string]bool, len(opts.Config.EntityTypes))
		for _, t := range opts.Config.EntityTypes {
			typeSet[strings.ToUpper(t)] = true
		}
	}

	entities := make([]Entity, 0, len(proseEntities))
	seen := make(map[string]bool) // deduplicate by text+label

	for _, ent := range proseEntities {
		label := strings.ToUpper(ent.Label)
		canonicalType := mapEntityLabel(label)

		// Skip unmapped entity types.
		if canonicalType == "" {
			continue
		}

		// Skip if type filter is set and entity doesn't match (check against canonical type).
		if typeSet != nil && !typeSet[canonicalType] {
			continue
		}

		// Skip entities with low confidence.
		if ent.Confidence < opts.Config.MinConfidence {
			continue
		}

		// LOCATION entities need higher confidence to avoid common nouns.
		if canonicalType == "LOCATION" && ent.Confidence < opts.Config.LocationMinConfidence {
			continue
		}

		// Skip very short entities that are likely common nouns (except PERSON).
		if len(ent.Text) < 3 && canonicalType != "PERSON" {
			continue
		}

		key := ent.Text + "|" + canonicalType
		if seen[key] {
			continue
		}
		seen[key] = true

		entities = append(entities, Entity{
			Name:       ent.Text,
			Type:       canonicalType,
			Confidence: ent.Confidence, // v3 provides actual confidence scores
		})
	}

	return entities
}

func mapEntityLabel(label string) string {
	switch label {
	case "PERSON":
		return "PERSON"
	case "ORGANIZATION", "ORG":
		return "ORGANIZATION"
	case "GPE", "FAC":
		return "LOCATION" // GPE (geopolitical entity), FAC (facility) map to LOCATION
	case "MONEY", "PERCENT":
		return "QUANTITY" // monetary amounts and percentages
	case "DATE", "TIME":
		return "TIMEFRAME" // dates and times
	case "CARDINAL", "ORDINAL":
		return "VALUE" // numbers, levels (Junior, Mid, Senior)
	case "LAW":
		return "DOCUMENT" // policies, regulations
	case "NORP":
		return "GROUP" // nationalities, organizations, religious/political groups
	case "PRODUCT", "EVENT", "WORK_OF_ART", "LANGUAGE":
		// Less common types — pass through as-is for extensibility.
		return label
	default:
		return "" // skip unsupported/unmapped types to reduce noise
	}
}
