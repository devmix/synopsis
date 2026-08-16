package expression

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
)

// Engine provides compile-and-evaluate functionality for CEL expressions.
// It is domain-agnostic: consumers register variables, functions, and scope loaders.
type Engine struct {
	env        *cel.Env
	scopeCache *ScopeCache
}

// NewEngine creates a new Engine with the given options.
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{
		scopeCache: NewScopeCache(),
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.env == nil {
		var err error
		e.env, err = cel.NewEnv()
		if err != nil {
			return nil, fmt.Errorf("create CEL environment: %w", err)
		}
	}

	return e, nil
}

// RegisterVariable declares a named variable with the given type.
// Uses cel.DynType for maximum flexibility — field access is resolved at runtime via reflection.
func (e *Engine) RegisterVariable(name string, _ interface{}) error {
	newEnv, err := e.env.Extend(cel.Variable(name, cel.DynType))
	if err != nil {
		return fmt.Errorf("register variable %q: %w", name, err)
	}
	e.env = newEnv
	return nil
}

// RegisterOverload adds function overloads to the CEL environment.
func (e *Engine) RegisterOverload(opts ...cel.EnvOption) error {
	newEnv, err := e.env.Extend(opts...)
	if err != nil {
		return fmt.Errorf("register overload: %w", err)
	}
	e.env = newEnv
	return nil
}

// Compile compiles an expression string and validates its return type.
func (e *Engine) Compile(name, expr string, returnType *cel.Type) (*CompiledExpr, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile expression %q: %w", name, issues.Err())
	}

	outputType := ast.OutputType()
	if outputType.String() != returnType.String() {
		return nil, fmt.Errorf(
			"expression %q return type mismatch: got %s, want %s",
			name, outputType, returnType,
		)
	}

	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("create program for expression %q: %w", name, err)
	}

	return &CompiledExpr{
		Name:       name,
		AST:        ast,
		Program:    prog,
		ReturnType: returnType,
	}, nil
}

// Evaluate runs a compiled expression against the provided bindings.
func (e *Engine) Evaluate(ctx context.Context, compiled *CompiledExpr, bindings map[string]interface{}) (interface{}, error) {
	out, details, err := compiled.Program.Eval(bindings)
	if err != nil {
		return nil, fmt.Errorf("evaluate expression %q: %w", compiled.Name, err)
	}

	_ = ctx // available for future use in function callbacks
	_ = details

	return out.Value(), nil
}

// ScopeCache returns the engine's scope cache for registering loaders.
func (e *Engine) ScopeCache() *ScopeCache {
	return e.scopeCache
}
