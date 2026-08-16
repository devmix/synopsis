package relations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/expression"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// ExpressionLinker evaluates CEL expressions to determine entity links.
type ExpressionLinker struct {
	engine   *expression.Engine
	programs map[string]*expression.CompiledExpr
	exprList []config.LinkExpression
	db       *sql.DB
}

// NewExpressionLinker creates a new linker bound to the database.
func NewExpressionLinker(db *sql.DB) *ExpressionLinker {
	return &ExpressionLinker{db: db}
}

// Init registers variables, functions, scope loaders and compiles expressions from config.
func (el *ExpressionLinker) Init(ctx context.Context, expressions []config.LinkExpression) error {
	eng, err := expression.NewEngine()
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	// Register variables A and B as EntityContext (DynType allows field access)
	if err := eng.RegisterVariable("A", EntityContext{}); err != nil {
		return fmt.Errorf("register variable A: %w", err)
	}
	if err := eng.RegisterVariable("B", EntityContext{}); err != nil {
		return fmt.Errorf("register variable B: %w", err)
	}

	// Register entity metadata function
	if err := registerMetadataFunction(eng); err != nil {
		return fmt.Errorf("register metadata function: %w", err)
	}

	sc := eng.ScopeCache()

	// Register domain-specific CEL functions
	if err := registerFactsFunction(eng, sc); err != nil {
		return fmt.Errorf("register facts function: %w", err)
	}
	if err := registerHasFactFunction(eng, sc); err != nil {
		return fmt.Errorf("register has_fact function: %w", err)
	}
	if err := registerChunksFunction(eng, sc); err != nil {
		return fmt.Errorf("register chunks function: %w", err)
	}
	if err := registerChunkContainsFunction(eng, sc); err != nil {
		return fmt.Errorf("register chunk_contains function: %w", err)
	}
	if err := registerNeighborsFunction(eng, sc); err != nil {
		return fmt.Errorf("register neighbors function: %w", err)
	}
	if err := registerPathExistsFunction(eng, sc); err != nil {
		return fmt.Errorf("register path_exists function: %w", err)
	}

	// Register scope loaders (lazy loading — data loaded on first function call)
	el.registerScopeLoaders(eng)

	// Compile expressions, sorted by priority (higher priority first)
	sorted := make([]config.LinkExpression, len(expressions))
	copy(sorted, expressions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	el.programs = make(map[string]*expression.CompiledExpr, len(sorted))
	for _, expr := range sorted {
		compiled, err := eng.Compile(expr.Name, expr.Where, cel.BoolType)
		if err != nil {
			return fmt.Errorf("compile expression %q: %w", expr.Name, err)
		}
		el.programs[expr.Name] = compiled
	}

	el.exprList = sorted
	el.engine = eng
	return nil
}

// EvaluatePair evaluates all expressions against an entity pair in priority order.
// Returns (matched, expressionName, error).
func (el *ExpressionLinker) EvaluatePair(ctx context.Context, a, b dao.Entity) (bool, string, error) {
	bindings := map[string]interface{}{
		"A": entityToMap(a),
		"B": entityToMap(b),
	}

	for _, expr := range el.exprList {
		compiled, ok := el.programs[expr.Name]
		if !ok {
			continue
		}
		result, err := el.engine.Evaluate(ctx, compiled, bindings)
		if err != nil {
			return false, "", fmt.Errorf("evaluate %q: %w", expr.Name, err)
		}
		if result.(bool) {
			return true, expr.Name, nil
		}
	}
	return false, "", nil
}

// entityToMap converts a dao.Entity to map[string]interface{} for CEL evaluation.
func entityToMap(e dao.Entity) map[string]interface{} {
	m := map[string]interface{}{
		"id":            e.ID,
		"type":          e.Type,
		"name":          e.Name,
		"domain":        e.Domain,
		"confidence":    e.Confidence,
		"description":   e.Description,
		"metadata_json": e.MetadataJSON,
		"created_at":    e.CreatedAt,
	}
	return m
}

func registerMetadataFunction(eng *expression.Engine) error {
	return eng.RegisterOverload(
		cel.Function("metadata",
			cel.Overload("metadata_dyn_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityMap, ok := args[0].Value().(map[string]interface{})
					if !ok {
						return types.NewErr("metadata: first argument must be an entity")
					}
					key, ok := args[1].Value().(string)
					if !ok {
						return types.NewErr("metadata: second argument must be a string")
					}
					metaJSONRaw, exists := entityMap["metadata_json"]
					if !exists || metaJSONRaw == nil {
						return types.String("")
					}
					metaJSON, ok := metaJSONRaw.(*string)
					if !ok || metaJSON == nil {
						return types.String("")
					}
					var m map[string]interface{}
					if err := json.Unmarshal([]byte(*metaJSON), &m); err != nil {
						return types.NewErr("metadata: invalid JSON")
					}
					v, ok := m[key].(string)
					if !ok {
						return types.String("")
					}
					return types.String(v)
				}),
			),
		),
	)
}

func registerFactsFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("facts",
			cel.Overload("facts_int_string",
				[]*cel.Type{cel.IntType, cel.StringType},
				cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))
					predicate := args[1].Value().(string)

					entry, err := sc.Get(context.Background(), "facts")
					if err != nil {
						return types.NewErr("facts: %v", err)
					}
					fi := entry.Data.(*FactIndex)
					results := fi.Lookup(entityID, predicate)

					elemVals := make([]ref.Val, 0, len(results))
					for _, f := range results {
						m := map[ref.Val]ref.Val{
							types.String("predicate"): types.String(f.Predicate),
							types.String("domain"):    types.String(f.Domain),
							types.String("id"):        types.Int(int64(f.ID)),
						}
						elemVals = append(elemVals, types.NewMutableMap(types.DefaultTypeAdapter, m))
					}
					return types.NewRefValList(types.DefaultTypeAdapter, elemVals)
				}),
			),
		),
	)
}

func registerHasFactFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("has_fact",
			cel.Overload("has_fact_int_string",
				[]*cel.Type{cel.IntType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))
					predicate := args[1].Value().(string)

					entry, err := sc.Get(context.Background(), "facts")
					if err != nil {
						return types.Bool(false)
					}
					fi := entry.Data.(*FactIndex)
					results := fi.Lookup(entityID, predicate)
					return types.Bool(len(results) > 0)
				}),
			),
			cel.Overload("has_fact_int_string_string",
				[]*cel.Type{cel.IntType, cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))
					predicate := args[1].Value().(string)
					value := args[2].Value().(string)

					entry, err := sc.Get(context.Background(), "facts")
					if err != nil {
						return types.Bool(false)
					}
					fi := entry.Data.(*FactIndex)
					results := fi.Lookup(entityID, predicate)
					for _, f := range results {
						if f.Predicate == value || f.Domain == value {
							return types.Bool(true)
						}
					}
					return types.Bool(false)
				}),
			),
		),
	)
}

func registerChunksFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("chunks",
			cel.Overload("chunks_int",
				[]*cel.Type{cel.IntType},
				cel.ListType(cel.StringType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))

					entry, err := sc.Get(context.Background(), "chunks")
					if err != nil {
						return types.NewErr("chunks: %v", err)
					}
					ci := entry.Data.(*ChunkIndex)
					texts := ci.Texts(entityID)

					elemVals := make([]ref.Val, 0, len(texts))
					for _, t := range texts {
						elemVals = append(elemVals, types.String(t))
					}
					return types.NewRefValList(types.DefaultTypeAdapter, elemVals)
				}),
			),
		),
	)
}

func registerChunkContainsFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("chunk_contains",
			cel.Overload("chunk_contains_int_string",
				[]*cel.Type{cel.IntType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))
					text := args[1].Value().(string)

					entry, err := sc.Get(context.Background(), "chunks")
					if err != nil {
						return types.Bool(false)
					}
					ci := entry.Data.(*ChunkIndex)
					return types.Bool(ci.Contains(entityID, text))
				}),
			),
		),
	)
}

func registerNeighborsFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("neighbors",
			cel.Overload("neighbors_int_int",
				[]*cel.Type{cel.IntType, cel.IntType},
				cel.ListType(cel.IntType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					entityID := int(args[0].Value().(int64))
					hops := int(args[1].Value().(int64))

					entry, err := sc.Get(context.Background(), "graph")
					if err != nil {
						return types.NewErr("neighbors: %v", err)
					}
					gi := entry.Data.(*GraphIndex)
					ids := gi.Neighbors(entityID, hops)

					elemVals := make([]ref.Val, 0, len(ids))
					for _, id := range ids {
						elemVals = append(elemVals, types.Int(int64(id)))
					}
					return types.NewRefValList(types.DefaultTypeAdapter, elemVals)
				}),
			),
		),
	)
}

func registerPathExistsFunction(eng *expression.Engine, sc *expression.ScopeCache) error {
	return eng.RegisterOverload(
		cel.Function("path_exists",
			cel.Overload("path_exists_int_int_int",
				[]*cel.Type{cel.IntType, cel.IntType, cel.IntType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					aID := int(args[0].Value().(int64))
					bID := int(args[1].Value().(int64))
					maxHops := int(args[2].Value().(int64))

					entry, err := sc.Get(context.Background(), "graph")
					if err != nil {
						return types.Bool(false)
					}
					gi := entry.Data.(*GraphIndex)
					return types.Bool(gi.PathExists(aID, bID, maxHops, ""))
				}),
			),
			cel.Overload("path_exists_int_int_int_string",
				[]*cel.Type{cel.IntType, cel.IntType, cel.IntType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					aID := int(args[0].Value().(int64))
					bID := int(args[1].Value().(int64))
					maxHops := int(args[2].Value().(int64))
					relType := args[3].Value().(string)

					entry, err := sc.Get(context.Background(), "graph")
					if err != nil {
						return types.Bool(false)
					}
					gi := entry.Data.(*GraphIndex)
					return types.Bool(gi.PathExists(aID, bID, maxHops, relType))
				}),
			),
		),
	)
}

func (el *ExpressionLinker) registerScopeLoaders(eng *expression.Engine) {
	eng.ScopeCache().RegisterLoader("facts", func(ctx context.Context) (interface{}, error) {
		return buildFactIndex(ctx, el.db)
	})
	eng.ScopeCache().RegisterLoader("chunks", func(ctx context.Context) (interface{}, error) {
		return buildChunkIndex(ctx, el.db)
	})
	eng.ScopeCache().RegisterLoader("graph", func(ctx context.Context) (interface{}, error) {
		return buildGraphIndex(ctx, el.db, 3)
	})
}
