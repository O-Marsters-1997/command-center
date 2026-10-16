package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestNormaliseRemote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "https", url: "https://github.com/Owner/Repo.git", want: "github.com/owner/repo"},
		{name: "scp ssh", url: "git@github.com:Owner/Repo.git", want: "github.com/owner/repo"},
		{name: "ssh scheme with user", url: "ssh://git@github.com/Owner/Repo/", want: "github.com/owner/repo"},
		{name: "padded", url: "  https://github.com/o/r  ", want: "github.com/o/r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plan.NormaliseRemote(tt.url); got != tt.want {
				t.Errorf("NormaliseRemote(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
