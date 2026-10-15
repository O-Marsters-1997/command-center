package view_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestGraphPreservesDecodedIslandFields(t *testing.T) {
	t.Parallel()
	row := view.Row{
		URL: "https://github.com/acme/web/issues/27", Title: "Ship", State: "ready",
		Glyph: plan.GlyphReady, Alive: true, Blocking: []string{"one"}, After: []string{"two"},
		Reason: "waiting", Elapsed: "2m", PRNumber: 12,
		Repo: "private", LogPath: "secret", SessionPath: "/s/outdated?feature=foo",
	}
	encoded, err := json.Marshal(view.Graph([]view.Group{{Root: option.Some(row), Children: []view.Row{row}}}))
	if err != nil {
		t.Fatalf("Marshal(Graph(rows)): %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal(Graph(rows)): %v", err)
	}
	wantRow := map[string]any{
		"url": row.URL, "ref": "#27", "title": "Ship", "state": "ready",
		"glyph": string(plan.GlyphReady), "alive": true, "blocking": []any{"one"},
		"after": []any{"two"}, "reason": "waiting", "elapsed": "2m",
		"pr_number": float64(12), "session_path": "/s/acme/web/27",
	}
	want := []map[string]any{{"root": wantRow, "children": []any{wantRow}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Graph(rows) mismatch (-want +got):\n%s", diff)
	}
}

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
