package store_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestStore_PlanInput(t *testing.T) {
	t.Parallel()
	const url = plan.TicketURL("sandbox://CC-1")
	ticket := storepkg.Ticket{URL: string(url), Repo: "cc-sandbox", Branch: "cc-1"}

	newStore := func(t *testing.T) *storepkg.Store {
		t.Helper()
		st := openStore(t)
		storetest.Tickets(t, st, ticket)
		return st
	}
	read := func(t *testing.T, st *storepkg.Store) plan.Input {
		t.Helper()
		in, err := st.PlanInput(t.Context())
		if err != nil {
			t.Fatalf("PlanInput: %v", err)
		}
		return in
	}

	t.Run("Intents carry id, verb and payload per ticket", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		if err := st.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: ticket.URL, Verb: plan.VerbFollowUp, Payload: "go on", At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		want := map[plan.TicketURL][]plan.Intent{url: {{Ticket: url, Verb: plan.VerbFollowUp, Payload: "go on"}}}
		if diff := cmp.Diff(want, read(t, st).Intents, cmpopts.IgnoreFields(plan.Intent{}, "ID")); diff != "" {
			t.Errorf("PlanInput().Intents mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Repos carry each repo's phase and refusal", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		repo := cloningRepo(t, st)
		want := []plan.RepoStatus{{Name: repo.Name, Phase: plan.RepoCloning}}
		if diff := cmp.Diff(want, read(t, st).Repos); diff != "" {
			t.Errorf("PlanInput().Repos mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Restacked marks a ticket restacked since its last push", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		appendEvents(t, st, storepkg.Event{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventRestacked})
		if diff := cmp.Diff(map[plan.TicketURL]bool{url: true}, read(t, st).Restacked); diff != "" {
			t.Errorf("PlanInput().Restacked mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Sessions carry the latest session of a ticket with a run", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		if got := read(t, st).Sessions; len(got) != 0 {
			t.Fatalf("PlanInput().Sessions with no runs = %v, want none", got)
		}
		_, session, err := st.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
			Ticket: ticket.URL, Kind: plan.RunKindAgent, Session: storepkg.RunSession{StartNew: true},
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton: %v", err)
		}
		if diff := cmp.Diff(map[plan.TicketURL]string{url: session}, read(t, st).Sessions); diff != "" {
			t.Errorf("PlanInput().Sessions mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Recorded reports which transition events exist", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		appendEvents(t, st, storepkg.Event{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventPROpened})
		want := map[plan.TicketURL]plan.Recorded{url: {Opened: true}}
		if diff := cmp.Diff(want, read(t, st).Recorded); diff != "" {
			t.Errorf("PlanInput().Recorded mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("LastVerdicts carry the saved verdict per ticket", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		if err := st.SaveLastVerdicts(t.Context(), map[string]string{ticket.URL: "review_me"}); err != nil {
			t.Fatalf("SaveLastVerdicts: %v", err)
		}
		if diff := cmp.Diff(map[plan.TicketURL]string{url: "review_me"}, read(t, st).LastVerdicts); diff != "" {
			t.Errorf("PlanInput().LastVerdicts mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Refreshes record an attempt and parse a conflict's tips", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		appendEvents(t, st, storepkg.Event{
			At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventRefreshConflicted,
			Detail: "branchtip basetip CONFLICT in a.go",
		})
		want := map[plan.TicketURL]plan.RefreshFact{url: {
			Tried:    true,
			Conflict: option.Some(plan.ConflictAt{BranchTip: "branchtip", BaseTip: "basetip"}),
		}}
		allow := cmp.AllowUnexported(option.Option[plan.ConflictAt]{})
		if diff := cmp.Diff(want, read(t, st).Refreshes, allow); diff != "" {
			t.Errorf("PlanInput().Refreshes mismatch (-want +got):\n%s", diff)
		}
	})
}
