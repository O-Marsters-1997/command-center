package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestPageOffersEveryLaunchableRowInOneLaunchForm(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)
	rec := do(t, server, http.MethodGet, "/tickets", req{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body
	wants := []string{
		`<form id="launch" method="post" action="/launch/open" hx-post="/launch/open"`,
		`<input type="checkbox" form="launch" name="ticket" value="sandbox://CC-1"`,
		`<input type="checkbox" form="launch" name="ticket" value="sandbox://CC-2"`,
		`<button type="submit" form="launch" class="btn">Launch selected</button>`,
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

func TestQueryChecksExactlyTheNamedTickets(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
		{URL: "sandbox://C", Repo: "repo", Branch: "c"},
	}
	storetest.Tickets(t, store, tickets...)
	now := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := openServer(track(t, store, named("repo")...), fixedClock(now), "")

	target := "/tickets?" + url.Values{"ticket": {"sandbox://A", "sandbox://B"}}.Encode()
	rec := do(t, server, http.MethodGet, target, req{})
	body := rec.Body

	for _, want := range []string{
		`value="sandbox://A" checked`,
		`value="sandbox://B" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="sandbox://C" checked`) {
		t.Errorf("sandbox://C is checked but was not named in ?ticket=:\n%s", body)
	}
}

func TestPageRendersTheVerbsOfEveryState(t *testing.T) {
	t.Parallel()

	verbForm := regexp.MustCompile(`name="verb" value="([a-z-]+)"`)
	for _, state := range plan.AllStates() {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()

			board, err := web.RenderStatesBoard([]plan.State{state})
			if err != nil {
				t.Fatal(err)
			}
			var got []plan.Verb
			for _, m := range verbForm.FindAllStringSubmatch(board, -1) {
				got = append(got, plan.Verb(m[1]))
			}
			if want := wantRowVerbs[state]; !slices.Equal(got, want) {
				t.Errorf("verb forms for %s = %v, want %v", state, got, want)
			}
		})
	}
}

var wantRowVerbs = map[plan.State][]plan.Verb{
	plan.Queued:                  {plan.VerbCancel},
	plan.Running:                 {plan.VerbKill},
	plan.Failed:                  {plan.VerbReRun},
	plan.CutFailed:               {plan.VerbReRun},
	plan.Checking:                {plan.VerbReRun},
	plan.NeedsYou:                {plan.VerbReRun, plan.VerbKill},
	plan.PushFailed:              {plan.VerbRetryPush, plan.VerbReRun},
	plan.PRMerged:                {plan.VerbRemoveWorktree},
	plan.PRClosedUnmerged:        {plan.VerbReRun, plan.VerbRemoveWorktree},
	plan.BaseGone:                {plan.VerbReRun, plan.VerbRemoveWorktree},
	plan.BaseMoved:               {plan.VerbRefresh, plan.VerbReRun},
	plan.CIFailed:                {plan.VerbReRun},
	plan.RefreshConflicted:       {plan.VerbAbort},
	plan.ConflictsWithMain:       {plan.VerbRefresh},
	plan.VerificationFailed:      {plan.VerbRetryPush, plan.VerbReRun},
	plan.WaitingOnProducerDeploy: {plan.VerbReRun},
	plan.AwaitingInput:           {plan.VerbReRun},
}

func TestLaunchAcceptsRepeatedFormEncodedTickets(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	server := newServer(store, testNow)
	response := do(t, server, http.MethodPost, "/launch", req{
		Form: url.Values{"ticket": {"sandbox://CC-1", "sandbox://CC-2"}},
	})
	if response.Code != http.StatusSeeOther || response.Location != "/" {
		t.Errorf("POST /launch = (%d, %q), want (303, /)", response.Code, response.Location)
	}

	ctx := t.Context()
	if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
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
