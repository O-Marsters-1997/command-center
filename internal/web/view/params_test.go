package view_test

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestParseParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  view.Params
	}{
		{name: "empty", want: view.Params{Log: view.LogAll}},
		{
			name:  "a stale sel is ignored beside every ticket",
			query: "sel=a&ticket=x&ticket=y",
			want:  view.Params{Tickets: []string{"x", "y"}, Log: view.LogAll},
		},
		{name: "a named log filter passes through", query: "log=fails", want: view.Params{Log: view.LogFails}},
		{name: "an unrecognised log filter falls back to all", query: "log=bogus", want: view.Params{Log: view.LogAll}},
		{name: "repo raw", query: "repo=support-app", want: view.Params{Repo: "support-app", Log: view.LogAll}},
		{name: "feature raw", query: "feature=cc-220", want: view.Params{Feature: "cc-220", Log: view.LogAll}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q): %v", tt.query, err)
			}
			if diff := cmp.Diff(tt.want, view.ParseParams(q)); diff != "" {
				t.Errorf("ParseParams(%q) mismatch (-want +got):\n%s", tt.query, diff)
			}
		})
	}
}

func TestParams_Query(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    view.Params
		want string
	}{
		{name: "empty"},
		{name: "tickets", v: view.Params{Tickets: []string{"b", "c"}}, want: "ticket=b&ticket=c"},
		{name: "a non-default log filter", v: view.Params{Log: view.LogFails}, want: "log=fails"},
		{name: "the default log filter is never written", v: view.Params{Log: view.LogAll}},
		{
			name: "a repo scope",
			v:    view.Params{Log: view.LogFails, Repo: "support-app"},
			want: "log=fails&repo=support-app",
		},
		{
			name: "a project scope",
			v:    view.Params{Log: view.LogFails, Project: "acme"},
			want: "log=fails&project=acme",
		},
		{
			name: "a feature scope beside a repo scope",
			v:    view.Params{Log: view.LogFails, Repo: "support-app", Feature: "board-scope"},
			want: "feature=board-scope&log=fails&repo=support-app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.v.Query()
			if got != tt.want {
				t.Errorf("%+v.Query() = %q, want %q", tt.v, got, tt.want)
			}
			q, err := url.ParseQuery(got)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q): %v", got, err)
			}
			if diff := cmp.Diff(tt.v.Query(), view.ParseParams(q).Query()); diff != "" {
				t.Errorf("ParseParams(%q).Query() round trip mismatch (-want +got):\n%s", got, diff)
			}
		})
	}
}

func TestParams_BoardPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v    view.Params
		want string
	}{
		{v: view.Params{}, want: "/board"},
		{v: view.Params{Feature: "a"}, want: "/board?feature=a"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.v.BoardPath(); got != tt.want {
				t.Errorf("%+v.BoardPath() = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestParams_PagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    view.Params
		want string
	}{
		{name: "unscoped", want: "/tickets"},
		{
			name: "all tickets keep their filter",
			v:    view.Params{All: true, Filter: "blocked"},
			want: "/tickets?filter=blocked",
		},
		{name: "a feature", v: view.Params{Feature: "project:x", Tickets: []string{"a"}}, want: "/f/project:x?ticket=a"},
		{name: "a repo", v: view.Params{Repo: "acme/web"}, want: "/repos/acme/web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.v.PagePath(); got != tt.want {
				t.Errorf("%+v.PagePath() = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestParams_ToggleTicket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ticket string
		want   []string
	}{
		{ticket: "c", want: []string{"a", "b", "c"}},
		{ticket: "a", want: []string{"b"}},
	}
	for _, tt := range tests {
		t.Run(tt.ticket, func(t *testing.T) {
			t.Parallel()
			base := view.Params{Tickets: []string{"a", "b"}}

			got := base.ToggleTicket(tt.ticket)

			if diff := cmp.Diff(tt.want, got.Tickets); diff != "" {
				t.Errorf("ToggleTicket(%q) mismatch (-want +got):\n%s", tt.ticket, diff)
			}
			if diff := cmp.Diff([]string{"a", "b"}, base.Tickets); diff != "" {
				t.Errorf("ToggleTicket(%q) mutated the receiver (-want +got):\n%s", tt.ticket, diff)
			}
		})
	}
}

func TestParams_Scoped(t *testing.T) {
	t.Parallel()

	repos := []store.Repo{{Name: "acme/support-app"}, {Name: "acme/services"}}
	acme := store.Project{Name: "acme", Hue: 2}
	projects := []store.Project{acme}
	tickets := []store.Ticket{{Feature: "sqlc-migration"}, {Feature: "board-scope"}, {Feature: ""}}

	tests := []struct {
		name        string
		in          view.Params
		want        view.Params
		wantProject store.Project
	}{
		{name: "absent stays blank"},
		{
			name: "a tracked repo passes through",
			in:   view.Params{Repo: "acme/support-app"}, want: view.Params{Repo: "acme/support-app"},
		},
		{name: "an untracked repo falls back to blank", in: view.Params{Repo: "bogus"}},
		{
			name: "a tracked repo in another case reads as its tracked name",
			in:   view.Params{Repo: "ACME/Support-App"}, want: view.Params{Repo: "acme/support-app"},
		},
		{
			name: "a project in another case reads as its name",
			in:   view.Params{Project: "ACME"}, want: view.Params{Project: "acme"}, wantProject: acme,
		},
		{name: "an unknown project falls back to blank", in: view.Params{Project: "nope"}},
		{
			name: "a feature in the fleet passes through",
			in:   view.Params{Feature: "board-scope"}, want: view.Params{Feature: "board-scope"},
		},
		{name: "a feature not in the fleet falls back to blank", in: view.Params{Feature: "bogus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, project := tt.in.Scoped(repos, projects, tickets)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%+v.Scoped() params mismatch (-want +got):\n%s", tt.in, diff)
			}
			if diff := cmp.Diff(tt.wantProject, project); diff != "" {
				t.Errorf("%+v.Scoped() project mismatch (-want +got):\n%s", tt.in, diff)
			}
		})
	}
}
