package prompts

import (
	"strings"
	"text/template"

	"github.com/devmix/synopsis/internal/utils"
)

// FuncMap returns a template.FuncMap with utility functions for prompt templates.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"join":     func(sep string, s []string) string { return strings.Join(s, sep) },
		"truncate": utils.Truncate,
	}
}
