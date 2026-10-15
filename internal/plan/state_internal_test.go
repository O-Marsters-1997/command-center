package plan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestEveryNamedStateHasARow(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "plan.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseFile(plan.go): %v", err)
	}

	var named []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		first, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok {
			continue
		}
		if ident, ok := first.Type.(*ast.Ident); !ok || ident.Name != "State" {
			continue
		}
		for _, spec := range gen.Specs {
			named = append(named, spec.(*ast.ValueSpec).Names[0].Name)
		}
	}
	if len(named) < 2 || named[0] != "StateUnknown" {
		t.Fatalf("State consts = %v, want StateUnknown first", named)
	}

	for i, name := range named[1:] {
		if _, ok := stateInfo[State(i+1)]; !ok {
			t.Errorf("State %s has no stateInfo row", name)
		}
	}
	if len(stateInfo) != len(named)-1 {
		t.Errorf("stateInfo has %d rows, plan.go names %d states", len(stateInfo), len(named)-1)
	}
}

func TestParseStateRoundTrips(t *testing.T) {
	t.Parallel()

	for _, s := range AllStates() {
		got, err := ParseState(s.String())
		if err != nil || got != s {
			t.Errorf("ParseState(%q) = %v, %v; want %v", s.String(), got, err, s)
		}
	}
	if _, err := ParseState("no_such_state"); err == nil {
		t.Error("ParseState(no_such_state) succeeded, want an error")
	}
}
