package storetest

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
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
}

func Run(tb testing.TB, st *store.Store, o RunOpts) (runID int64) {
	tb.Helper()
	var err error
	runID, _, err = st.InsertSessionRunSkeleton(tb.Context(), store.RunSkeleton{
		Ticket: o.TicketURL, Kind: o.Kind, Session: o.Session,
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
			RunID: plan.RunID(runID), Outcome: o.Outcome.Unwrap(), EndedAt: o.EndedAt,
		}); err != nil {
			tb.Fatalf("RecordDisposition: %v", err)
		}
	}
	return runID
}
