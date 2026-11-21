package storetest

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func Tickets(tb testing.TB, st *store.Store, tickets ...store.Ticket) {
	tb.Helper()
	if err := st.UpsertTickets(tb.Context(), tickets); err != nil {
		tb.Fatalf("UpsertTickets: %v", err)
	}
	if err := st.SaveObservation(tb.Context(), plan.Observation{}); err != nil {
		tb.Fatalf("SaveObservation: %v", err)
	}
}

func FeatureTicket(tb testing.TB, st *store.Store, url, repo, feature string) {
	tb.Helper()
	Tickets(tb, st, store.Ticket{URL: url, Repo: repo, Branch: "branch-" + url, Feature: feature})
}

func Disposed(tb testing.TB, st *store.Store, url string, kind plan.RunKind, endedAt time.Time, costUSD float64) {
	tb.Helper()
	Run(tb, st, RunOpts{
		TicketURL: url, Kind: kind, BaselineSHA: "deadbeef", PromptHash: "hash-" + url,
		Outcome: option.Some(plan.OutcomePush), Cost: option.Some(costUSD), EndedAt: endedAt,
	})
}

func Merged(tb testing.TB, st *store.Store, url string, at time.Time) {
	tb.Helper()
	if err := st.AppendEvent(tb.Context(), store.Event{At: at, TicketURL: url, Kind: store.EventPRMerged}); err != nil {
		tb.Fatalf("AppendEvent %s: %v", store.EventPRMerged, err)
	}
}

func Verb(tb testing.TB, st *store.Store, v store.VerbRequest) int64 {
	tb.Helper()
	id, err := st.QueueVerbIntent(tb.Context(), v)
	if err != nil {
		tb.Fatalf("QueueVerbIntent(%s, %s): %v", v.Ticket, v.Verb, err)
	}
	return id
}

func Events(tb testing.TB, st *store.Store, kind store.EventKind) []store.Event {
	tb.Helper()
	all, err := st.Events(tb.Context())
	if err != nil {
		tb.Fatalf("Events: %v", err)
	}
	if kind == "" {
		return all
	}
	var selected []store.Event
	for _, e := range all {
		if e.Kind == kind {
			selected = append(selected, e)
		}
	}
	return selected
}

type PushedOpts struct {
	URL, Tip, Base, BaseTip string
	At                      time.Time
}

func Pushed(tb testing.TB, st *store.Store, o PushedOpts) {
	tb.Helper()
	if err := st.RecordPush(tb.Context(), store.Push{
		Ticket: plan.TicketURL(o.URL), Tip: plan.SHA(o.Tip), BaseBranch: o.Base,
		BaseSHA: plan.SHA(o.BaseTip), At: o.At,
	}); err != nil {
		tb.Fatalf("RecordPush: %v", err)
	}
}

type RunOpts struct {
	TicketURL string
	Kind      plan.RunKind
	Session   store.RunSession
	Pgid      int
	StartedAt time.Time
	LogPath   string
	Outcome   option.Option[plan.Outcome]
	EndedAt   time.Time
	ExitCode  option.Option[int]
	Cost      option.Option[float64]

	PromptHash   string
	BaselineSHA  string
	Step         plan.StepName
	Workflow     string
	WorkflowHash string

	Alive      bool
	ObservedAt time.Time
}

func Run(tb testing.TB, st *store.Store, o RunOpts) (runID int64) {
	tb.Helper()
	var err error
	runID, _, err = st.InsertSessionRunSkeleton(tb.Context(), store.RunSkeleton{
		Ticket: o.TicketURL, Kind: o.Kind, Session: o.Session, PromptHash: o.PromptHash,
		BaselineSHA: o.BaselineSHA, Step: o.Step, Workflow: o.Workflow, WorkflowHash: o.WorkflowHash,
	})
	if err != nil {
		tb.Fatalf("InsertSessionRunSkeleton: %v", err)
	}
	if !o.StartedAt.IsZero() {
		if err := st.RecordSpawn(tb.Context(), store.Spawn{
			RunID: plan.RunID(runID), Pgid: o.Pgid, StartedAt: o.StartedAt, LogPath: o.LogPath,
		}); err != nil {
			tb.Fatalf("RecordSpawn: %v", err)
		}
	}
	if o.Outcome.IsSome() {
		if err := st.RecordDisposition(tb.Context(), store.Disposition{
			RunID: plan.RunID(runID), Outcome: o.Outcome.Unwrap(), ExitCode: o.ExitCode, Cost: o.Cost, EndedAt: o.EndedAt,
		}); err != nil {
			tb.Fatalf("RecordDisposition: %v", err)
		}
	}
	if o.Alive {
		obs := plan.Observation{
			ObservedAt: o.ObservedAt,
			Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(o.TicketURL): {Alive: true}},
		}
		if err := st.SaveObservation(tb.Context(), obs); err != nil {
			tb.Fatalf("SaveObservation: %v", err)
		}
	}
	return runID
}
