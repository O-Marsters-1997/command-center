package tracker

import "testing"

func TestNew(t *testing.T) {
	t.Parallel()

	src, err := New(GitHub, "github.com/o-marsters-1997/command-center")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if src == nil {
		t.Error("New(GitHub) = nil source, want a source")
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   Kind
		remote string
	}{
		{name: "unknown kind", kind: "linear", remote: "github.com/owner/repo"},
		{name: "empty remote", kind: GitHub, remote: ""},
		{name: "owner only", kind: GitHub, remote: "owner-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.kind, tt.remote); err == nil {
				t.Errorf("New(%q, %q) = nil error, want one", tt.kind, tt.remote)
			}
		})
	}
}
