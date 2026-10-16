package web_test

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

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

func testBoardForms(t *testing.T) {
	t.Parallel()

	t.Run("offers every launchable row in one launch form", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow)))

		form := root.One(t, "form#launch")
		want := map[string]string{"method": "post", "action": "/launch/open", "hx-post": "/launch/open"}
		if diff := cmp.Diff(want, attrs(form, "method", "action", "hx-post")); diff != "" {
			t.Errorf("GET /tickets launch form mismatch (-want +got):\n%s", diff)
		}
		board := root.One(t, "table#board")
		for _, ticket := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
			board.One(t, `input[type="checkbox"][form="launch"][name="ticket"][value="`+ticket+`"]`)
		}
		root.One(t, `button[form="launch"][type="submit"]`)
		if wrong := board.Find(`button[value="launch"]`); len(wrong) != 0 {
			t.Errorf("GET /tickets posts launch to /verb: %s", wrong[0].HTML())
		}
	})

	t.Run("the query checks exactly the named tickets", func(t *testing.T) {
		t.Parallel()
		store := observedTickets(t,
			storepkg.Ticket{URL: "sandbox://A", Repo: "repo", Branch: "a"},
			storepkg.Ticket{URL: "sandbox://B", Repo: "repo", Branch: "b"},
			storepkg.Ticket{URL: "sandbox://C", Repo: "repo", Branch: "c"},
		)
		server := openServer(track(t, store, named("repo")...), fixedClock(testNow), "")
		target := "/tickets?" + url.Values{"ticket": {"sandbox://A", "sandbox://B"}}.Encode()

		board := htmltest.Parse(t, renderPath(t, server, target)).One(t, "table#board")
		got := map[string]bool{}
		for _, ticket := range []string{"sandbox://A", "sandbox://B", "sandbox://C"} {
			_, got[ticket] = board.One(t, `input[name="ticket"][value="`+ticket+`"]`).Attr("checked")
		}
		want := map[string]bool{"sandbox://A": true, "sandbox://B": true, "sandbox://C": false}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET %s checked tickets mismatch (-want +got):\n%s", target, diff)
		}
	})

	for _, state := range plan.AllStates() {
		t.Run("verbs of "+state.String(), func(t *testing.T) {
			t.Parallel()
			board, err := web.RenderStatesBoard([]plan.State{state})
			if err != nil {
				t.Fatalf("RenderStatesBoard(%s): %v", state, err)
			}
			var got []plan.Verb
			for _, button := range htmltest.Parse(t, board).One(t, "table#board").Find(`button[name="verb"]`) {
				value, _ := button.Attr("value")
				got = append(got, plan.Verb(value))
			}
			if diff := cmp.Diff(wantRowVerbs[state], got); diff != "" {
				t.Errorf("RenderStatesBoard(%s) verbs mismatch (-want +got):\n%s", state, diff)
			}
		})
	}
}
