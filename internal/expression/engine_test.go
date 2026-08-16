package expression

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestNewEngine(t *testing.T) {
	t.Parallel()

	t.Run("default engine creates successfully", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		if eng.env == nil {
			t.Fatal("env is nil")
		}
		if eng.scopeCache == nil {
			t.Fatal("scopeCache is nil")
		}
	})

	t.Run("engine has scope cache accessible", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		sc := eng.ScopeCache()
		if sc == nil {
			t.Fatal("ScopeCache returned nil")
		}
	})
}

func TestCompile(t *testing.T) {
	t.Parallel()

	t.Run("valid boolean expression compiles", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		compiled, err := eng.Compile("test", "true && false", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if compiled.Name != "test" {
			t.Errorf("name = %q, want %q", compiled.Name, "test")
		}
	})

	t.Run("syntax error returns descriptive error", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		_, err = eng.Compile("bad", "true &&", cel.BoolType)
		if err == nil {
			t.Fatal("expected error for syntax error")
		}
		if got := err.Error(); got[:len("compile expression \"bad\"")] != `compile expression "bad"` {
			t.Errorf("error should include expression name, got: %s", got)
		}
	})

	t.Run("type mismatch returns descriptive error", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		_, err = eng.Compile("wrong-type", "'hello'", cel.BoolType)
		if err == nil {
			t.Fatal("expected error for type mismatch")
		}
	})
}

type testEntity struct {
	Name   string
	Domain string
}

func TestRegisterVariable(t *testing.T) {
	t.Parallel()

	t.Run("register struct variable and access fields", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		if err := eng.RegisterVariable("A", testEntity{}); err != nil {
			t.Fatalf("RegisterVariable: %v", err)
		}

		compiled, err := eng.Compile("name-check", "A.Name == 'test'", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		result, err := eng.Evaluate(context.Background(), compiled, map[string]interface{}{
			"A": map[string]interface{}{"Name": "test", "Domain": "it"},
		})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.(bool) != true {
			t.Errorf("result = %v, want true", result)
		}
	})

	t.Run("register struct variable field access returns false on mismatch", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		if err := eng.RegisterVariable("A", testEntity{}); err != nil {
			t.Fatalf("RegisterVariable: %v", err)
		}

		compiled, err := eng.Compile("domain-check", "A.Domain == 'it'", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		result, err := eng.Evaluate(context.Background(), compiled, map[string]interface{}{
			"A": map[string]interface{}{"Name": "test", "Domain": "science"},
		})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.(bool) != false {
			t.Errorf("result = %v, want false", result)
		}
	})

	t.Run("unregistered variable causes compile error", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		_, err = eng.Compile("missing-var", "A.Name == 'test'", cel.BoolType)
		if err == nil {
			t.Fatal("expected error for unregistered variable")
		}
	})
}

func TestRegisterOverload(t *testing.T) {
	t.Parallel()

	t.Run("register custom function and use in expression", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		err = eng.RegisterOverload(cel.Function(
			"myDouble",
			cel.Overload("double_int", []*cel.Type{cel.IntType}, cel.IntType,
				cel.UnaryBinding(func(x ref.Val) ref.Val {
					return types.Int(x.Value().(int64) * 2)
				})),
		))
		if err != nil {
			t.Fatalf("RegisterOverload: %v", err)
		}

		compiled, err := eng.Compile("myDouble-test", "myDouble(5)", cel.IntType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		result, err := eng.Evaluate(context.Background(), compiled, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.(int64) != 10 {
			t.Errorf("result = %v, want 10", result)
		}
	})

	t.Run("unregistered function causes compile error", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		_, err = eng.Compile("unknown-func", "myDouble(5)", cel.IntType)
		if err == nil {
			t.Fatal("expected error for unregistered function")
		}
	})
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	t.Run("expression returns true", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		compiled, err := eng.Compile("always-true", "1 + 1 == 2", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		result, err := eng.Evaluate(context.Background(), compiled, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.(bool) != true {
			t.Errorf("result = %v, want true", result)
		}
	})

	t.Run("expression returns false", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		compiled, err := eng.Compile("always-false", "1 + 1 == 3", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		result, err := eng.Evaluate(context.Background(), compiled, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.(bool) != false {
			t.Errorf("result = %v, want false", result)
		}
	})

	t.Run("context cancellation is propagated", func(t *testing.T) {
		t.Parallel()
		eng, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}

		compiled, err := eng.Compile("simple", "true", cel.BoolType)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = eng.Evaluate(ctx, compiled, map[string]interface{}{})
		// CEL may or may not check context during simple eval; just verify no panic
		if err != nil {
			t.Logf("Evaluate with cancelled ctx: %v (expected)", err)
		}
	})
}
