package view_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestGraph(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(view.Graph([]view.Group{{}}))
	if err != nil {
		t.Fatalf("Marshal(Graph(empty group)): %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(Graph(empty group)): %v", err)
	}
	want := []map[string]any{{"root": nil, "children": []any{}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Graph(empty group) JSON mismatch (-want +got):\n%s", diff)
	}
}
