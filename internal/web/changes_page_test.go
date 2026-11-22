package web_test

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

var awaitingSettings = map[string]plan.RepoSettings{"acme/web": {Raise: plan.RaiseManual}}

func changesFixture(t *testing.T, tip func(repo string) string) (*storepkg.Store, string) {
	t.Helper()
	return changesFixtureAs(t, plan.OutcomeFailed, nil, tip)
}

func changesFixtureAs(
	t *testing.T, outcome plan.Outcome, settings map[string]plan.RepoSettings, tip func(repo string) string,
) (*storepkg.Store, string) {
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

	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", Outcome: outcome})
	obs := plan.Observation{
		ObservedAt: testNow,
		Settings:   settings,
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

	t.Run("the tree lists each folder with its count, kind dot and a link to the file card", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(repo string) string { return gittest.Run(t, repo, "rev-parse", "cc-1") })
		server := openServer(st, fixedClock(testNow), dataDir)

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		counts := page.Find(".tree-dir [data-dir-count]")
		if len(counts) != 2 || counts[0].Text() != "0/1" {
			t.Fatalf("GET changes folder counts = %d, want 2 folders reading 0/1", len(counts))
		}
		if got := len(page.Find(".tree-file .tree-kind-added")); got != 2 {
			t.Errorf("GET changes added dots = %d, want 2", got)
		}
		link := page.One(t, `.tree-file a[href="#f-1"]`)
		if link.Text() != "a.go" {
			t.Errorf("GET changes tree link f-1 = %q, want a.go", link.Text())
		}
		page.One(t, "details.diff-file#f-1")
	})

	t.Run("rows, toolbar and tree carry the hooks the changes script needs", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(repo string) string { return gittest.Run(t, repo, "rev-parse", "cc-1") })
		server := openServer(st, fixedClock(testNow), dataDir)

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		page.One(t, `script[type="module"][src="/assets/dist/changes.js"]`)
		page.One(t, `details.diff-file#f-1[data-kind="added"]`)
		if got := len(page.Find("tr.diff-line[data-kind=added][data-side=new][data-line]")); got != 2 {
			t.Errorf("GET changes annotated rows = %d, want 2", got)
		}
		page.One(t, "#changes-ws")
		page.One(t, "#changes-wrap")
		page.One(t, "#changes-filter")
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

	t.Run("a comment persists on its line and deleting it updates the count", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(repo string) string { return gittest.Run(t, repo, "rev-parse", "cc-1") })
		server := openServer(st, fixedClock(testNow), dataDir)

		form := url.Values{"path": {"src/a.go"}, "side": {"new"}, "line": {"1"}, "body": {"rename this"}}
		posted := do(t, server, http.MethodPost, "/s/acme/web/1/changes/comments", req{Form: form})
		if posted.Code != http.StatusOK || !strings.Contains(posted.Body, "Review changes (1)") {
			t.Fatalf("POST comment = %d %q, want 200 with a count of 1", posted.Code, posted.Body)
		}

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		threads := page.Find(".review-thread")
		if len(threads) != 1 || !strings.Contains(threads[0].Text(), "rename this") ||
			!strings.Contains(threads[0].Text(), "Pending") {
			t.Fatalf("GET changes threads = %d, want one Pending thread reading %q", len(threads), "rename this")
		}
		if got := page.One(t, "#review-changes").Text(); got != "Review changes (1)" {
			t.Errorf("GET changes review button = %q, want %q", got, "Review changes (1)")
		}

		comments, err := st.ReviewComments(t.Context(), sessionTicket)
		if err != nil || len(comments) != 1 || !strings.HasPrefix(comments[0].Hunk, "@@") ||
			!strings.HasSuffix(comments[0].Hunk, "+package a") {
			t.Fatalf("ReviewComments = %+v, %v, want one with the hunk through the line", comments, err)
		}
		deletePath := "/s/acme/web/1/changes/comments/" + strconv.FormatInt(comments[0].ID, 10) + "/delete"
		deleted := do(t, server, http.MethodPost, deletePath, req{})
		if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body, ">Review changes<") {
			t.Fatalf("POST delete = %d %q, want 200 with no count", deleted.Code, deleted.Body)
		}
		page = htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		if got := len(page.Find(".review-thread")); got != 0 {
			t.Errorf("GET changes threads after delete = %d, want 0", got)
		}
	})

	t.Run("request changes queues one follow-up quoting the comments and clears them", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(repo string) string { return gittest.Run(t, repo, "rev-parse", "cc-1") })
		server := openServer(st, fixedClock(testNow), dataDir)
		form := url.Values{"path": {"src/a.go"}, "side": {"new"}, "line": {"1"}, "body": {"rename this"}}
		do(t, server, http.MethodPost, "/s/acme/web/1/changes/comments", req{Form: form})

		note := url.Values{"note": {"mind the API"}}
		rec := do(t, server, http.MethodPost, "/s/acme/web/1/changes/request", req{Form: note})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST request = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		comments, err := st.ReviewComments(t.Context(), sessionTicket)
		if err != nil || len(comments) != 0 {
			t.Errorf("ReviewComments after request = %v, %v, want none", comments, err)
		}
		intents, err := st.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
		if err != nil || len(intents) != 1 {
			t.Fatalf("PendingVerbIntents(follow-up) = %v, %v, want one", intents, err)
		}
		payload := storepkg.DecodeFollowUpPayload(intents[0].Payload)
		if payload.Message != "mind the API" {
			t.Errorf("follow-up message = %q, want the note alone", payload.Message)
		}
		if len(payload.Comments) != 1 {
			t.Fatalf("follow-up comments = %v, want one", payload.Comments)
		}
		got := payload.Comments[0]
		if got.Path != "src/a.go" || got.Line != 1 || got.Side != plan.SideNew || got.Body != "rename this" ||
			!strings.Contains(got.Hunk, "+package a") {
			t.Errorf("follow-up comment = %+v, want src/a.go new line 1 %q on its hunk", got, "rename this")
		}
	})

	t.Run("request changes is refused with the reason where follow-up is not offered", func(t *testing.T) {
		t.Parallel()
		var sha string
		st, dataDir := changesFixture(t, func(repo string) string {
			sha = gittest.Run(t, repo, "rev-parse", "cc-1")
			return sha
		})
		obs := plan.Observation{
			ObservedAt: testNow,
			Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {}},
			LocalTips:  map[plan.BranchRef]plan.SHA{branchRef("acme/web", "cc-1"): plan.SHA(sha)},
			PRs:        map[plan.BranchRef]plan.PR{branchRef("acme/web", "cc-1"): {Number: 7, State: plan.Merged}},
		}
		if err := st.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		server := openServer(st, fixedClock(testNow), dataDir)

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		if _, disabled := page.One(t, "#review-popover button[type=submit]").Attr("disabled"); !disabled {
			t.Error("GET changes Request changes button is enabled, want disabled")
		}
		if got := page.One(t, "#review-popover").Text(); !strings.Contains(got, "merged") {
			t.Errorf("GET changes popover = %q, want the state's reason", got)
		}
		rec := do(t, server, http.MethodPost, "/s/acme/web/1/changes/request", req{Form: url.Values{"note": {"x"}}})
		if rec.Code != http.StatusConflict {
			t.Errorf("POST request = %d, want %d", rec.Code, http.StatusConflict)
		}
		intents, err := st.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
		if err != nil || len(intents) != 0 {
			t.Errorf("PendingVerbIntents(follow-up) = %v, %v, want none", intents, err)
		}
	})

	t.Run("a comment on a line outside the diff is refused", func(t *testing.T) {
		t.Parallel()
		st, dataDir := changesFixture(t, func(repo string) string { return gittest.Run(t, repo, "rev-parse", "cc-1") })
		form := url.Values{"path": {"src/a.go"}, "side": {"new"}, "line": {"99"}, "body": {"x"}}
		server := openServer(st, fixedClock(testNow), dataDir)
		rec := do(t, server, http.MethodPost, "/s/acme/web/1/changes/comments", req{Form: form})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST comment on line 99 = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("raise queues the posted sha, clears comments and returns the PR link once consumed", func(t *testing.T) {
		t.Parallel()
		var sha string
		st, dataDir := changesFixtureAs(t, plan.OutcomePush, awaitingSettings, func(repo string) string {
			sha = gittest.Run(t, repo, "rev-parse", "cc-1")
			return sha
		})
		server := openServer(st, fixedClock(testNow), dataDir)
		form := url.Values{"path": {"src/a.go"}, "side": {"new"}, "line": {"1"}, "body": {"rename this"}}
		do(t, server, http.MethodPost, "/s/acme/web/1/changes/comments", req{Form: form})

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		got := page.One(t, "#review-popover").Text()
		if !strings.Contains(got, "Raise PR") || !strings.Contains(got, "discards") {
			t.Errorf("GET changes popover = %q, want Raise PR and the discard warning", got)
		}

		consumer := make(chan error, 1)
		go func() {
			poll := time.NewTicker(10 * time.Millisecond)
			defer poll.Stop()
			for ; t.Context().Err() == nil; <-poll.C {
				intents, err := st.PendingVerbIntents(t.Context(), plan.VerbRaise)
				if err != nil {
					consumer <- err
					return
				}
				if len(intents) > 0 {
					if got := intents[0].Payload; got != sha {
						consumer <- errors.New("raise payload = " + got + ", want " + sha)
						return
					}
					push := storepkg.Push{
						Ticket: sessionTicket, Tip: plan.SHA(sha), BaseSHA: plan.SHA(sha), BaseBranch: "main", At: testNow,
					}
					if err := st.RecordPush(t.Context(), push); err != nil {
						consumer <- err
						return
					}
					consumer <- st.ConsumeVerbIntent(t.Context(), intents[0].ID, storepkg.Consumption{At: testNow})
					return
				}
			}
		}()
		hx := http.Header{"Hx-Request": {"true"}}
		rec := do(t, server, http.MethodPost, "/s/acme/web/1/changes/raise", req{Form: url.Values{"sha": {sha}}, Header: hx})
		if err := <-consumer; err != nil {
			t.Fatalf("consuming the raise intent: %v", err)
		}
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body, "Pull request") {
			t.Fatalf("POST raise = %d %q, want 200 with a PR link", rec.Code, rec.Body)
		}
		comments, err := st.ReviewComments(t.Context(), sessionTicket)
		if err != nil || len(comments) != 0 {
			t.Errorf("ReviewComments after raise = %v, %v, want none", comments, err)
		}
	})

	t.Run("raise is refused where the ticket is not awaiting PR", func(t *testing.T) {
		t.Parallel()
		var sha string
		st, dataDir := changesFixture(t, func(repo string) string {
			sha = gittest.Run(t, repo, "rev-parse", "cc-1")
			return sha
		})
		server := openServer(st, fixedClock(testNow), dataDir)
		rec := do(t, server, http.MethodPost, "/s/acme/web/1/changes/raise", req{Form: url.Values{"sha": {sha}}})
		if rec.Code != http.StatusConflict {
			t.Errorf("POST raise = %d, want %d", rec.Code, http.StatusConflict)
		}
		intents, err := st.PendingVerbIntents(t.Context(), plan.VerbRaise)
		if err != nil || len(intents) != 0 {
			t.Errorf("PendingVerbIntents(raise) = %v, %v, want none", intents, err)
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
