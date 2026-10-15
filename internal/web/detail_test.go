package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func detailStore(t *testing.T, logPath string, startedAt, now time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "https://github.com/o/r/issues/76", Repo: "repo", Branch: "cc-76"}
	storetest.Tickets(t, store, ticket)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: "basesha1234", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%s): %v", ticket.URL, err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: startedAt, LogPath: logPath,
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: true}},
		Worktrees:  map[plan.BranchRef]string{branchRef("repo", "cc-76"): "/repos/repo-cc-76"},
		PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "cc-76"): {
			Number: 76, State: plan.Open, HeadRef: "cc-76",
			Checks: map[string]plan.CheckState{
				"unit":  {Status: "COMPLETED", Conclusion: "SUCCESS"},
				"build": {Status: "IN_PROGRESS", DetailsURL: "https://github.com/o/r/actions/runs/1"},
			},
		}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func writeLog(t *testing.T, n int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-08-20T12:00:%02dZ",`+
			`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"step %d"}}]}}`+"\n",
			i%60, i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func sessionPagePath(ticketURL string) string { return view.SessionPath(ticketURL) }

func TestTheDeletedDetailRouteIs404(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	for _, target := range []string{
		"/ticket/" + url.PathEscape("sandbox://CC-1") + "/detail",
		"/task/" + url.PathEscape("sandbox://CC-1") + "/detail",
	} {
		rec := do(t, server, http.MethodGet, target, req{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

func TestHTMXIsServedFromTheBinary(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, "/assets/htmx.min.js", req{})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/htmx.min.js = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body, "htmx") {
		t.Error("/assets/htmx.min.js does not serve htmx")
	}

	page := do(t, server, http.MethodGet, "/tickets", req{})
	body := page.Body
	root := htmltest.Parse(t, body)
	root.One(t, `script[src="/assets/htmx.min.js"]`)
	for _, script := range root.Find("script[src]") {
		if src, _ := script.Attr("src"); strings.HasPrefix(src, "//") || strings.HasPrefix(src, "http") {
			t.Errorf("page loads a network script: %s", script.HTML())
		}
	}
}

func TestBoardRowLinksToItsSession(t *testing.T) {
	t.Parallel()

	now := testNow
	body := renderPath(t, newServer(detailStore(t, writeLog(t, 1), now, now), now), "/tickets")
	htmltest.Parse(t, body).One(t, `table#board a[href="/s/o/r/76"]`)
}

func TestBoardPollsItselfInsteadOfReloading(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, "/tickets", req{})
	body := rec.Body

	root := htmltest.Parse(t, body)
	board := root.One(t, "table#board")
	attrs := map[string]string{
		"hx-get": "/board?all=1", "hx-trigger": "every 5s",
		"hx-sync": "this:drop", "hx-swap": "outerHTML",
	}
	for name, want := range attrs {
		if got, _ := board.Attr(name); got != want {
			t.Errorf("board %s = %q, want %q", name, got, want)
		}
	}
	if refresh := root.Find(`meta[http-equiv="refresh"]`); len(refresh) != 0 {
		t.Errorf("page has meta refresh: %s", refresh[0].HTML())
	}
	if selects := root.Find(`[hx-select="#board"]`); len(selects) != 0 {
		t.Errorf("board poll selects from a full document: %s", selects[0].HTML())
	}
}

func TestVerbsNeedNoJavaScript(t *testing.T) {
	t.Parallel()

	states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
	for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
		states = append(states, s)
	}
	board, err := web.RenderStatesPage(states)
	if err != nil {
		t.Fatalf("RenderStatesPage: %v", err)
	}

	root := htmltest.Parse(t, board)
	verbForms := root.Find(`form[action="/verb"][method="post"][hx-post]`)
	if len(verbForms) == 0 {
		t.Error("board has no scriptless verb form")
	}
	launch := root.One(t, `form#launch[method="post"][action="/launch/open"][hx-post="/launch/open"]`)
	if selections := root.Find(`table#board input[type="checkbox"][form="launch"][name="ticket"]`); len(selections) == 0 {
		t.Error("board has no scriptless ticket selections")
	}
	if got, want := len(root.Find("[hx-post]")), len(verbForms)+1; got != want {
		t.Errorf("hx-post elements = %d, want %d verb forms and %s", got, want, launch.HTML())
	}
	var tags []string
	seen := make(map[string]bool)
	markup := html.NewTokenizer(strings.NewReader(board))
	for tokenType := markup.Next(); tokenType != html.ErrorToken; tokenType = markup.Next() {
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		tag := markup.Token().Data
		if !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	for _, tag := range tags {
		for _, element := range root.Find(tag) {
			tokenizer := html.NewTokenizer(strings.NewReader(element.HTML()))
			tokenType := tokenizer.Next()
			if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
				t.Errorf("cannot inspect %s element", tag)
				continue
			}
			startTag := tokenizer.Token()
			var hasHTMX, posts bool
			for _, attr := range startTag.Attr {
				if strings.HasPrefix(attr.Key, "hx-") {
					hasHTMX = true
				}
				if attr.Key == "hx-post" {
					posts = true
				}
			}
			if !hasHTMX {
				continue
			}
			method, _ := element.Attr("method")
			action, _ := element.Attr("action")
			id, _ := element.Attr("id")
			target, _ := element.Attr("hx-target")
			formPosts := tag == "form" && method == "post" && (action == "/verb" || action == "/launch/open")
			allowed := tag == "table" || tag == "div" || tag == "tr" || tag == "section" ||
				(tag == "a" && !posts) || (tag == "input" && id == "rail-search") ||
				((tag == "input" || tag == "button") && target == "#board") || formPosts
			if !allowed {
				t.Errorf("verb control needs JavaScript: %s", startTag.String())
			}
			if posts && !formPosts {
				t.Errorf("verb posts over htmx rather than a form: %s", startTag.String())
			}
		}
	}
}

func TestDroppedKindsNeverReachTheRender(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := writeRunLog(t, mustReadTestdata(t, "dropped_kinds.jsonl"))
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := do(t, server, http.MethodGet, sessionPagePath(ticket), req{})
	page := htmltest.Parse(t, rec.Body).One(t, "main#main")

	for _, dropped := range []string{"deadbeef-session", "pondering-deeply"} {
		if strings.Contains(page.Text(), dropped) {
			t.Errorf("dropped log kind reached session text: %q", dropped)
		}
	}
	if !strings.Contains(page.Text(), "echo hi") {
		t.Error("real event absent from session text: want echo hi")
	}
	if !strings.Contains(page.Text(), "lines 4") {
		t.Error("session line count missing lines 4")
	}
}

func TestBoardPollIntervalComesFromTheServer(t *testing.T) {
	t.Parallel()

	now := testNow
	store := seededStore(t, now)
	server := newServer(store, now)
	board := htmltest.Parse(t, boardBody(t, server)).One(t, "table#board")
	if got, _ := board.Attr("hx-trigger"); got != "every 5s" {
		t.Errorf("default board hx-trigger = %q, want every 5s", got)
	}

	server = newServer(store, now, web.WithBoardPollSeconds(1))
	board = htmltest.Parse(t, boardBody(t, server)).One(t, "table#board")
	if got, _ := board.Attr("hx-trigger"); got != "every 1s" {
		t.Errorf("board hx-trigger = %q, want every 1s", got)
	}
}

func boardBody(t *testing.T, server *web.Server) string {
	t.Helper()
	rec := do(t, server, http.MethodGet, "/board", req{Header: http.Header{"Hx-Request": {"true"}}})
	return rec.Body
}
