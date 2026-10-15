package web_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
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
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body
	board := htmltest.Parse(t, body).One(t, "table#board")
	form := htmltest.Parse(t, body).One(t, "form#launch")
	for name, want := range map[string]string{"method": "post", "action": "/launch/open", "hx-post": "/launch/open"} {
		if got, _ := form.Attr(name); got != want {
			t.Errorf("launch form %s = %q, want %q: %s", name, got, want, form.HTML())
		}
	}
	for _, ticket := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
		board.One(t, `input[type="checkbox"][form="launch"][name="ticket"][value="`+ticket+`"]`)
	}
	htmltest.Parse(t, body).One(t, `button[form="launch"][type="submit"]`)
	if wrong := board.Find(`button[value="launch"]`); len(wrong) != 0 {
		t.Errorf("launch is posted to /verb: %s", wrong[0].HTML())
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

	board := htmltest.Parse(t, body).One(t, "table#board")
	for _, ticket := range []string{"sandbox://A", "sandbox://B"} {
		board.One(t, `input[name="ticket"][value="`+ticket+`"][checked]`)
	}
	board.One(t, `input[name="ticket"][value="sandbox://C"]`)
	if checked := board.Find(`input[name="ticket"][value="sandbox://C"][checked]`); len(checked) != 0 {
		t.Errorf("unnamed ticket is checked: %s", checked[0].HTML())
	}
}

func TestPageRendersTheVerbsOfEveryState(t *testing.T) {
	t.Parallel()

	for _, state := range plan.AllStates() {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()

			board, err := web.RenderStatesBoard([]plan.State{state})
			if err != nil {
				t.Fatal(err)
			}
			var got []plan.Verb
			for _, button := range htmltest.Parse(t, board).One(t, "table#board").Find(`button[name="verb"]`) {
				value, _ := button.Attr("value")
				got = append(got, plan.Verb(value))
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
