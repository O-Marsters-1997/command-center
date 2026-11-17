package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

const failingRunURL = "https://github.com/acme/web/actions/runs/9/job/1"

func ciFailedStore(t *testing.T) *storepkg.Store {
	t.Helper()
	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", Outcome: plan.OutcomePush})
	if err := st.RecordPush(t.Context(), storepkg.Push{
		Ticket: sessionTicket, Tip: "tip", BaseSHA: "base", BaseBranch: "main", At: testNow,
	}); err != nil {
		t.Fatalf("RecordPush(%q): %v", sessionTicket, err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {}},
		PRs: map[plan.BranchRef]plan.PR{branchRef("acme/web", "cc-1"): {
			Number: 7, State: plan.Open, HeadOid: "tip", HeadRef: "cc-1",
			Checks: map[string]plan.CheckState{
				"unit": {Status: "COMPLETED", Conclusion: "SUCCESS"},
				"lint": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: failingRunURL},
			},
		}},
		BranchTips: map[plan.BranchRef]plan.SHA{branchRef("acme/web", plan.DefaultBaseBranch): "base"},
		Settings: map[string]config.RepoSettings{"acme/web": {Checks: verdict.Predicate{AllOf: []verdict.Predicate{
			{Success: "unit"}, {Success: "lint"},
		}}}},
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st
}

func TestSessionPage_FixCI(t *testing.T) {
	t.Parallel()

	t.Run("a ci_failed session opens a dialog holding the generated prompt", func(t *testing.T) {
		t.Parallel()
		main := sessionMain(t, ciFailedStore(t), "/s/acme/web/1")

		main.One(t, `#session-actions button[data-show-modal="fix-ci-dialog"][data-key="i"]`)
		if inside := main.One(t, "#session-actions").Find("dialog"); len(inside) != 0 {
			t.Error("GET /s/acme/web/1 renders the dialog inside the polled #session-actions")
		}
		dialog := main.One(t, "dialog#fix-ci-dialog")
		dialog.One(t, `input[name="verb"][value="follow-up"]`)
		dialog.One(t, `select[name="model"]`)
		dialog.One(t, `select[name="effort"]`)
		prompt := dialog.One(t, `textarea[name="prompt"]`).Text()
		for _, want := range []string{"lint", failingRunURL} {
			if !strings.Contains(prompt, want) {
				t.Errorf("fix CI prompt = %q, want it to contain %q", prompt, want)
			}
		}
		if strings.Contains(prompt, "unit") {
			t.Errorf("fix CI prompt = %q, want only the failing checks", prompt)
		}
		if got := dialog.Text(); !strings.Contains(got, "attached") {
			t.Errorf("fix CI dialog = %q, want it to say the failed logs are attached on send", got)
		}
	})

	t.Run("the polled actions refresh the dialog out of band", func(t *testing.T) {
		t.Parallel()
		fragment := htmltest.Parse(t, renderPath(t, newServer(ciFailedStore(t), testNow), "/s/acme/web/1/actions"))

		fragment.One(t, `dialog#fix-ci-dialog[hx-swap-oob]`)
		page := sessionMain(t, ciFailedStore(t), "/s/acme/web/1")
		if got := page.Find(`dialog#fix-ci-dialog[hx-swap-oob]`); len(got) != 0 {
			t.Errorf("GET /s/acme/web/1 marks the dialog out of band, which an hx-select navigation would swap away")
		}
	})

	t.Run("any other state offers no Fix CI", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true})
		main := sessionMain(t, st, "/s/acme/web/1")

		if got := main.Find(`[data-show-modal="fix-ci-dialog"], dialog#fix-ci-dialog`); len(got) != 0 {
			t.Errorf("GET /s/acme/web/1 offers Fix CI outside ci_failed: %s", got[0].HTML())
		}
	})
}
