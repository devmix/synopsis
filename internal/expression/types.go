package expression

import (
	"context"
	"time"

	"github.com/google/cel-go/cel"
)

// CompiledExpr holds a compiled and optimized CEL expression ready for evaluation.
type CompiledExpr struct {
	Name       string
	AST        *cel.Ast
	Program    cel.Program
	ReturnType *cel.Type
}

// ScopeEntry holds cached scope data built by a ScopeBuilder.
type ScopeEntry struct {
	Data    interface{}
	BuiltAt time.Time
	TTL     time.Duration
}

// IsExpired returns true if the entry has exceeded its TTL. Zero TTL means never expires.
func (e *ScopeEntry) IsExpired() bool {
	if e.TTL == 0 {
		return false
	}
	return time.Since(e.BuiltAt) > e.TTL
}

// ScopeBuilder is a function that builds scope data on first access.
type ScopeBuilder func(ctx context.Context) (interface{}, error)

// Option configures the Engine.
type Option func(*Engine)
