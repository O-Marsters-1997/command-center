package option_test

import (
	"encoding/json"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/option"
)

func TestOptionEquality(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b option.Option[int]
		want bool
	}{
		{"none equals none", option.None[int](), option.None[int](), true},
		{"some equals same some", option.Some(3), option.Some(3), true},
		{"some differs from other some", option.Some(3), option.Some(4), false},
		{"some zero differs from none", option.Some(0), option.None[int](), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.a == tt.b; got != tt.want {
				t.Errorf("%v == %v = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestOptionAccessors(t *testing.T) {
	t.Parallel()
	if s := option.Some(2); !s.IsSome() || s.IsNone() || s.Unwrap() != 2 || s.UnwrapOr(9) != 2 {
		t.Errorf("Some(2) misbehaves: %v", s)
	}
	if n := option.None[int](); n.IsSome() || !n.IsNone() || n.UnwrapOr(9) != 9 {
		t.Errorf("None misbehaves: %v", n)
	}
}

func TestOptionJSON(t *testing.T) {
	t.Parallel()
	type doc struct {
		A option.Option[int]    `json:"a"`
		B option.Option[string] `json:"b"`
	}
	in := doc{A: option.Some(0), B: option.None[string]()}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal(option document): %v", err)
	}
	if got, want := string(b), `{"a":0,"b":null}`; got != want {
		t.Fatalf("marshal = %s, want %s", got, want)
	}
	var out doc
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal(option document): %v", err)
	}
	if out != in {
		t.Errorf("round trip = %v, want %v", out, in)
	}
}

func TestUnwrapNonePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Unwrap on None did not panic")
		}
	}()
	option.None[int]().Unwrap()
}
