package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func changesFixture(t *testing.T, tip func(repo string) string) (*storepkg.Store, string) {
	t.Helper()
	repo := gittest.Init(t)
	gittest.Run(t, repo, "update-ref", "refs/remotes/origin/main", "main")
	gittest.Run(t, repo, "switch", "-q", "-c", "cc-1")
	gittest.Commit(t, repo, ".github/ci.yml", "on: push\n")
	gittest.Commit(t, repo, "src/a.go", "package a\n")

	dataDir := t.TempDir()
	checkout := config.CheckoutPath(dataDir, "acme/web")
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(checkout), err)
	}
	if err := os.Symlink(repo, checkout); err != nil {
		t.Fatalf("Symlink(%q): %v", checkout, err)
	}

	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", Outcome: plan.OutcomeFailed})
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {}},
		LocalTips:  map[plan.BranchRef]plan.SHA{},
	}
	if sha := tip(repo); sha != "" {
		obs.LocalTips[branchRef("acme/web", "cc-1")] = plan.SHA(sha)
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st, dataDir
}

func TestChangesPage(t *testing.T) {
	t.Parallel()

	t.Run("lists every changed file with totals, protected flags and the viewed key", func(t *testing.T) {
		t.Parallel()
		var sha string
		st, dataDir := changesFixture(t, func(repo string) string {
			sha = gittest.Run(t, repo, "rev-parse", "cc-1")
			return sha
		})
		server := openServer(st, fixedClock(testNow), dataDir)

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		if got := len(page.Find("details.diff-file")); got != 2 {
			t.Errorf("GET /s/acme/web/1/changes files = %d, want 2", got)
		}
		if got := page.One(t, ".changes-toolbar").Text(); !strings.Contains(got, "2 files") || !strings.Contains(got, "+2") {
			t.Errorf("GET /s/acme/web/1/changes toolbar = %q, want 2 files and +2", got)
		}
		flagged := page.Find("details.diff-file .flag-warning")
		if len(flagged) != 1 || flagged[0].Text() != "protected" {
			t.Errorf("GET /s/acme/web/1/changes protected flags = %d, want one reading %q", len(flagged), "protected")
		}
		wantKey := "cc:viewed:" + sessionTicket + ":" + sha
		if got, _ := page.One(t, "[data-changes]").Attr("data-viewed-key"); got != wantKey {
			t.Errorf("GET /s/acme/web/1/changes data-viewed-key = %q, want %q", got, wantKey)
		}
	})

	t.Run("the session page links to it from a Changes tab", func(t *testing.T) {
		t.Parallel()
		st, _ := changesFixture(t, func(string) string { return "" })
		tabs := sessionMain(t, st, "/s/acme/web/1").Find(".pill-tab")
		last := tabs[len(tabs)-1]
		if href, _ := last.Attr("href"); last.Text() != "Changes" || href != "/s/acme/web/1/changes" {
			t.Errorf("GET /s/acme/web/1 last tab = %q -> %q, want Changes -> /s/acme/web/1/changes", last.Text(), href)
		}
	})

	t.Run("a ticket with no local tip is not found", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(string) string { return "" })
		rec := do(t, openServer(st, fixedClock(testNow), dataDir), http.MethodGet, "/s/acme/web/1/changes", req{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /s/acme/web/1/changes status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
