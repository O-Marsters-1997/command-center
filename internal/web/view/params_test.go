package view

import (
	"net/url"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestParseViewParamsTakesTheFirstSel(t *testing.T) {
	t.Parallel()

	q, err := url.ParseQuery("sel=a&sel=b&ticket=x&ticket=y")
	if err != nil {
		t.Fatal(err)
	}
	got := ParseParams(q)
	want := Params{Sel: "a", Tickets: []string{"x", "y"}}
	if got.Sel != want.Sel || len(got.Tickets) != 2 ||
		got.Tickets[0] != "x" || got.Tickets[1] != "y" {
		t.Errorf("ParseParams(%q) = %+v, want %+v", q, got, want)
	}
}

func TestParseViewParamsNormalizesTheLogFilter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		query string
		want  LogFilter
	}{
		{name: "absent defaults to all", want: LogAll},
		{name: "a named filter passes through", query: "log=fails", want: LogFails},
		{name: "an unrecognised value falls back to all", query: "log=bogus", want: LogAll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if got := ParseParams(q).Log; got != tc.want {
				t.Errorf("ParseParams(%q).Log = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestParseViewParamsReadsRepoRaw(t *testing.T) {
	t.Parallel()

	q, err := url.ParseQuery("repo=support-app")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseParams(q).Repo; got != "support-app" {
		t.Errorf("ParseParams(%q).Repo = %q, want support-app", q, got)
	}
}

func TestParseViewParamsReadsFeatureRaw(t *testing.T) {
	t.Parallel()

	q, err := url.ParseQuery("feature=cc-220")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseParams(q).Feature; got != "cc-220" {
		t.Errorf("ParseParams(%q).Feature = %q, want cc-220", q, got)
	}
}

func TestNormalizeFeatureScope(t *testing.T) {
	t.Parallel()

	fleet := []string{"board-scope", "sqlc-migration"}
	for _, tc := range []struct {
		name    string
		feature string
		want    string
	}{
		{name: "absent stays blank"},
		{name: "a feature in the fleet passes through", feature: "board-scope", want: "board-scope"},
		{name: "a feature not in the fleet falls back to blank", feature: "bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeFeatureScope(tc.feature, fleet); got != tc.want {
				t.Errorf("normalizeFeatureScope(%q) = %q, want %q", tc.feature, got, tc.want)
			}
		})
	}
}

func TestNormalizeRepoScope(t *testing.T) {
	t.Parallel()

	repos := []store.Repo{{Name: "acme/support-app"}, {Name: "acme/services"}}
	for _, tc := range []struct {
		name string
		repo string
		want string
	}{
		{name: "absent stays blank"},
		{name: "a tracked repo passes through", repo: "acme/support-app", want: "acme/support-app"},
		{name: "an untracked repo falls back to blank", repo: "bogus"},
		{
			name: "a tracked repo in another case reads as its tracked name",
			repo: "ACME/Support-App", want: "acme/support-app",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeRepoScope(tc.repo, repos); got != tc.want {
				t.Errorf("normalizeRepoScope(%q) = %q, want %q", tc.repo, got, tc.want)
			}
		})
	}
}

func TestViewParamsQueryRoundTripsThroughParse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		v    Params
		want string
	}{
		{name: "empty"},
		{name: "sel only", v: Params{Sel: "https://x/1"}, want: "sel=https%3A%2F%2Fx%2F1"},
		{name: "sel and tickets", v: Params{Sel: "a", Tickets: []string{"b", "c"}}, want: "sel=a&ticket=b&ticket=c"},
		{
			name: "a non-default log filter, alphabetically ahead of sel",
			v:    Params{Sel: "a", Log: "fails"},
			want: "log=fails&sel=a",
		},
		{name: "the default log filter is never written", v: Params{Log: "all"}},
		{
			name: "a repo scope sits between log and sel",
			v:    Params{Sel: "a", Log: "fails", Repo: "support-app"},
			want: "log=fails&repo=support-app&sel=a",
		},
		{
			name: "a project scope sits between log and sel",
			v:    Params{Sel: "a", Log: "fails", Project: "acme"},
			want: "log=fails&project=acme&sel=a",
		},
		{
			name: "a feature scope sorts ahead of log and repo, neither overriding the other",
			v:    Params{Sel: "a", Log: "fails", Repo: "support-app", Feature: "board-scope"},
			want: "feature=board-scope&log=fails&repo=support-app&sel=a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.v.query(); got != tc.want {
				t.Errorf("query() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestViewParamsBoardPathOmitsTheQuestionMarkWhenEmpty(t *testing.T) {
	t.Parallel()

	if got := (Params{}).boardPath(); got != "/board" {
		t.Errorf("boardPath() = %q, want /board", got)
	}
	if got := (Params{Sel: "a"}).boardPath(); got != "/board?sel=a" {
		t.Errorf("boardPath() = %q, want /board?sel=a", got)
	}
}

func TestViewParamsPagePathIsThePathRouteForTheScope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		v    Params
		want string
	}{
		{name: "unscoped", want: "/tickets"},
		{name: "all tickets keep their filter", v: Params{All: true, Filter: "blocked"}, want: "/tickets?filter=blocked"},
		{name: "a feature", v: Params{Feature: "project:x", Tickets: []string{"a"}}, want: "/f/project:x?ticket=a"},
		{name: "a repo", v: Params{Repo: "acme/web"}, want: "/repos/acme/web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.v.pagePath(); got != tc.want {
				t.Errorf("pagePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestViewParamsToggleTicketAddsThenRemoves(t *testing.T) {
	t.Parallel()

	base := Params{Tickets: []string{"a", "b"}}
	added := base.toggleTicket("c")
	if want := []string{"a", "b", "c"}; !equalStrings(added.Tickets, want) {
		t.Errorf("toggleTicket(c) = %v, want %v", added.Tickets, want)
	}
	removed := base.toggleTicket("a")
	if want := []string{"b"}; !equalStrings(removed.Tickets, want) {
		t.Errorf("toggleTicket(a) = %v, want %v", removed.Tickets, want)
	}
	if want := []string{"a", "b"}; !equalStrings(base.Tickets, want) {
		t.Errorf("toggleTicket mutated the receiver's own slice: Tickets = %v", base.Tickets)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
