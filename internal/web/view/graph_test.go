package view_test

import (
	"encoding/json"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestGraphMarshalsAbsentRootAndEmptyChildrenAsNullAndArray(t *testing.T) {
	t.Parallel()

	groups := view.Graph([]view.Group{{}})
	data, err := json.Marshal(groups)
	if err != nil {
		t.Fatalf("Marshal(Graph(empty group)): %v", err)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(Graph(empty group)): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Graph(empty group) = %d groups, want 1", len(got))
	}
	var root any
	if err := json.Unmarshal(got[0]["root"], &root); err != nil {
		t.Fatalf("Unmarshal(Graph(empty group).root): %v", err)
	}
	if root != nil {
		t.Errorf("Graph(empty group).root = %v, want nil", root)
	}
	var children []any
	if err := json.Unmarshal(got[0]["children"], &children); err != nil {
		t.Fatalf("Unmarshal(Graph(empty group).children): %v", err)
	}
	if children == nil || len(children) != 0 {
		t.Errorf("Graph(empty group).children = %v, want empty non-nil array", children)
	}
}
