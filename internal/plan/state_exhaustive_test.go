package plan_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStateStringIsDistinctAndOnlyBlockedReadsBlocked(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for i := range plan.StateCount {
		name := plan.State(i).String()
		if prev, dup := seen[name]; dup {
			t.Errorf("State(%d).String() = %q, same as State(%d)", i, name, prev)
		}
		seen[name] = i
		if name == "blocked" && plan.State(i) != plan.Blocked {
			t.Errorf("State(%d).String() = %q, want a case of its own", i, name)
		}
	}
	if got := plan.Blocked.String(); got != "blocked" {
		t.Errorf("Blocked.String() = %q, want %q", got, "blocked")
	}
}

func TestUnattendedNamesEveryState(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	planFile, err := parser.ParseFile(fset, "plan.go", nil, 0)
	if err != nil {
		t.Fatalf("parse plan.go: %v", err)
	}
	verbsFile, err := parser.ParseFile(fset, "verbs.go", nil, 0)
	if err != nil {
		t.Fatalf("parse verbs.go: %v", err)
	}

	var states []string
	ast.Inspect(planFile, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || spec.Names[0].Name == "stateCount" || !isStateSpec(spec, states) {
			return true
		}
		states = append(states, spec.Names[0].Name)
		return true
	})
	if len(states) != plan.StateCount {
		t.Fatalf("found %d state constants in plan.go, the enum has %d", len(states), plan.StateCount)
	}

	var named []string
	ast.Inspect(verbsFile, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Unattended" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if clause, ok := n.(*ast.CaseClause); ok {
				for _, expr := range clause.List {
					if id, ok := expr.(*ast.Ident); ok {
						named = append(named, id.Name)
					}
				}
			}
			return true
		})
		return false
	})

	for _, state := range states {
		switch n := countOf(named, state); n {
		case 1:
		case 0:
			t.Errorf("Unattended has no case for %s", state)
		default:
			t.Errorf("Unattended names %s %d times", state, n)
		}
	}
}

func isStateSpec(spec *ast.ValueSpec, seen []string) bool {
	if len(seen) == 0 {
		id, ok := spec.Type.(*ast.Ident)
		return ok && id.Name == "State"
	}
	return spec.Type == nil && len(spec.Values) == 0
}

func countOf(names []string, want string) int {
	return len(slices.DeleteFunc(slices.Clone(names), func(s string) bool { return s != want }))
}
