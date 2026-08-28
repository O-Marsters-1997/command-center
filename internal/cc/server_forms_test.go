package cc_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const goldenVerbsBoard = "testdata/board_verbs.golden.html"

func TestPageOffersEveryLaunchableRowInOneLaunchForm(t *testing.T) {
	t.Parallel()

	// seededStore's CC-1 derives ready and CC-2 blocked, and both states offer launch.
	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	wants := []string{
		`<form id="launch" method="get" action="/preview"></form>`,
		`<input type="checkbox" form="launch" name="task" value="sandbox://CC-1"`,
		`<input type="checkbox" form="launch" name="task" value="sandbox://CC-2"`,
		`<button type="submit" form="launch">launch selected</button>`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="launch"`) {
		t.Errorf("launch is posted to /verb rather than /launch:\n%s", body)
	}
}

// A third launchable row proves "exactly" those two, not just "at least".
func TestQueryChecksExactlyTheNamedTasks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
		{URL: "sandbox://C", Repo: "repo", Branch: "c"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := cc.NewServer(store, fixedClock(now), []cc.Repo{{Name: "repo"}}, "")

	target := "/?" + url.Values{"task": {"sandbox://A", "sandbox://B"}}.Encode()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	body := rec.Body.String()

	for _, want := range []string{
		`value="sandbox://A" checked`,
		`value="sandbox://B" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="sandbox://C" checked`) {
		t.Errorf("sandbox://C is checked but was not named in ?task=:\n%s", body)
	}
}

func TestPageRendersTheVerbsOfEveryState(t *testing.T) {
	t.Parallel()

	states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
	for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
		states = append(states, s)
	}

	got, err := cc.RenderStatesBoard(states)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, goldenVerbsBoard, []byte(got))
}

func TestLaunchAcceptsRepeatedFormEncodedTickets(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"task": {"sandbox://CC-1", "sandbox://CC-2"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

	ctx := t.Context()
	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if memberships["sandbox://CC-1"].LaunchID == 0 || memberships["sandbox://CC-2"].LaunchID == 0 {
		t.Fatalf("launch memberships = %v, want both tickets authorised", memberships)
	}
}
