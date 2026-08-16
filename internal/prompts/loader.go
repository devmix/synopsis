// Package prompts provides a runtime-loadable prompt template system.
// Templates are loaded from .tmpl files at startup; embedded defaults serve as fallback.
package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/devmix/synopsis/internal/logger"
)

// TemplateInfo holds a parsed template and its content hash.
type TemplateInfo struct {
	Template *template.Template
	Hash     string // SHA-256 hex digest of raw file content
}

// PromptLoader loads prompt templates from files with embedded fallback.
type PromptLoader struct {
	basePath string
	log      *logger.Logger
}

// NewLoader creates a loader that reads templates from basePath/*.tmpl.
func NewLoader(basePath string, log *logger.Logger) (*PromptLoader, error) {
	if basePath == "" {
		return nil, fmt.Errorf("prompts: base path is required")
	}
	return &PromptLoader{basePath: basePath, log: log}, nil
}

// Load reads and parses a template by name. The name should not include the .tmpl extension.
// If the file does not exist, it falls back to embedded defaults (with a warning).
func (l *PromptLoader) Load(name string) (*TemplateInfo, error) {
	path := filepath.Join(l.basePath, name+".tmpl")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if l.log != nil {
				l.log.Warn("prompt template file not found, using embedded default", "name", name, "path", path)
			}
			return nil, fmt.Errorf("prompts: read %s: %w", path, err)
		}
	}

	return parseTemplate(name, data)
}

func parseTemplate(name string, data []byte) (*TemplateInfo, error) {
	hash := sha256.Sum256(data)
	hexHash := hex.EncodeToString(hash[:])

	tmpl, err := template.New(name).Funcs(FuncMap()).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("prompts: parse %q: %w", name, err)
	}

	return &TemplateInfo{
		Template: tmpl,
		Hash:     hexHash,
	}, nil
}
