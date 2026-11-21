package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

const activityTicket = "https://github.com/acme/cc-sandbox/issues/1"

func seedIntent(t *testing.T, st *storepkg.Store, verb plan.Verb, at time.Time, c storepkg.Consumption) {
	t.Helper()
	id, err := st.QueueVerbIntent(t.Context(), storepkg.VerbRequest{Ticket: activityTicket, Verb: verb, At: at})
	if err != nil {
		t.Fatalf("QueueVerbIntent(%s): %v", verb, err)
	}
	if err := st.ConsumeVerbIntent(t.Context(), id, c); err != nil {
		t.Fatalf("ConsumeVerbIntent(%d): %v", id, err)
	}
}

func TestActivityPage(t *testing.T) {
	t.Parallel()

	seeded := func(t *testing.T) *storepkg.Store {
		t.Helper()
		st := track(t, openStore(t), named("cc-sandbox")...)
		storetest.Tickets(t, st, storepkg.Ticket{URL: activityTicket, Repo: "cc-sandbox", Branch: "cc-1", Feature: "feat-a"})
		at := testNow.Add(-time.Hour)
		old := at.Add(-45 * 24 * time.Hour)
		seedIntent(t, st, plan.VerbReRun, at, storepkg.Consumption{
			At: at.Add(26 * time.Second), Outcome: plan.IntentFailed, Detail: "refused: dirty worktree",
		})
		seedIntent(t, st, plan.VerbKill, old, storepkg.Consumption{At: old, Outcome: plan.IntentDone})
		return st
	}

	t.Run("the failed row shows its detail and an Open session link", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, newServer(seeded(t), testNow), "/activity?outcome=failed")
		main := htmltest.Parse(t, page).One(t, "main#main")

		if got := main.Text(); !strings.Contains(got, "refused: dirty worktree") {
			t.Errorf("GET /activity?outcome=failed text = %q, want the refusal detail", got)
		}
		main.One(t, `details a[href="/s/acme/cc-sandbox/1"]`)
	})

	t.Run("rows older than 30 days do not appear", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, newServer(seeded(t), testNow), "/activity")).One(t, "main#main")

		if got := main.Text(); strings.Contains(got, "kill") {
			t.Errorf("GET /activity text = %q, want the 45-day-old kill left out", got)
		}
	})

	t.Run("a filter with no match says so and offers Clear filters", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, newServer(seeded(t), testNow), "/activity?outcome=failed&q=zzzz")
		main := htmltest.Parse(t, page).One(t, "main#main")

		if got := main.Text(); !strings.Contains(got, "No failed actions match 'zzzz'") {
			t.Errorf("GET /activity empty text = %q, want the specific empty sentence", got)
		}
		main.One(t, `#activity-results a[href="/activity"]`)
	})
}
