package cc_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const goldenVerbsPage = "testdata/page_verbs.golden.html"

func TestPageOffersEveryLaunchableRowInOneLaunchForm(t *testing.T) {
	t.Parallel()

	// seededStore's CC-1 derives ready and CC-2 blocked, and both states offer launch.
	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	wants := []string{
		`<form id="launch" method="get" action="/preview"></form>`,
		`<input type="checkbox" form="launch" name="task" value="sandbox://CC-1">`,
		`<input type="checkbox" form="launch" name="task" value="sandbox://CC-2">`,
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

func TestPageRendersTheVerbsOfEveryState(t *testing.T) {
	t.Parallel()

	states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
	for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
		states = append(states, s)
	}

	got, err := cc.RenderStatesPage(states)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, goldenVerbsPage, []byte(got))
}

func TestLaunchAcceptsRepeatedFormEncodedTasks(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
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
		t.Fatalf("launch memberships = %v, want both tasks authorised", memberships)
	}
}
