package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.Context(), cctest.DSN(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

func TestInTxCancelledMidFnRollsBackAndReturnsFnsErrorAlone(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	boom := errors.New("boom")

	err := s.inTx(ctx, func(q *ccdb.Queries) error {
		_, err := q.CreateProject(ctx, ccdb.CreateProjectParams{Name: "partial", Hue: MinHue, CreatedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		cancel()
		return boom
	})
	if !errors.Is(err, boom) || errors.Is(err, context.Canceled) {
		t.Fatalf("inTx = %v, want boom alone", err)
	}

	_, err = s.q.GetProjectID(t.Context(), "partial")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ProjectID(partial) error = %v, want sql.ErrNoRows", err)
	}
}

func TestReadsThatDecideAWriteRunOnTheTransactionsConnection(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	s.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	url := "https://github.com/acme/alpha/issues/1"
	tickets := []ImportedTicket{
		{Ticket: tracker.Ticket{URL: url, Number: 1, Title: "One"}, Repo: "alpha", Source: "github"},
	}
	if err := s.ImportTickets(ctx, "project:x", tickets, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}
	if err := s.SaveObservation(ctx, plan.Observation{}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	if err := s.ImportTickets(ctx, "project:x", tickets, at); err != nil {
		t.Fatalf("ImportTickets with an observation: %v", err)
	}
	if err := s.EditTicket(ctx, url, "issue-1/one", nil); err != nil {
		t.Fatalf("EditTicket: %v", err)
	}
}
