package view_test

import (
	"encoding/json"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestGroupJSONEncodesAnAbsentRootAsNull(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(view.Group{Root: option.None[view.Row]()})
	if err != nil {
		t.Fatalf("json.Marshal(absent root): %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal(absent root): %v", err)
	}
	if string(got["root"]) != "null" {
		t.Errorf("root = %s, want null", got["root"])
	}
}

func TestGroupJSONRoundTripsARoot(t *testing.T) {
	t.Parallel()

	want := view.Group{Root: option.Some(view.Row{URL: "sandbox://CC-1", Title: "root"})}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(root): %v", err)
	}
	var got view.Group
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal(root): %v", err)
	}
	if !got.Root.IsSome() || got.Root.Unwrap().URL != "sandbox://CC-1" {
		t.Errorf("round trip root = %v, want %v", got.Root, want.Root)
	}
}
