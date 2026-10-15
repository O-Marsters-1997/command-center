package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestInTxCancelledMidFnRollsBackAndReturnsFnsErrorAlone(t *testing.T) {
	t.Parallel()

	s := cctest.Store(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	boom := errors.New("boom")

	err := s.TestCancelInTx(ctx, cancel, boom)
	if !errors.Is(err, boom) || errors.Is(err, context.Canceled) {
		t.Fatalf("inTx = %v, want boom alone", err)
	}

	var id int64
	err = s.DB().QueryRowContext(t.Context(), "SELECT id FROM projects WHERE name = $1", "partial").Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ProjectID(partial) error = %v, want sql.ErrNoRows", err)
	}
}

func TestReadsThatDecideAWriteRunOnTheTransactionsConnection(t *testing.T) {
	t.Parallel()

	s := cctest.Store(t)
	s.DB().SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	url := "https://github.com/acme/alpha/issues/1"
	tickets := []storepkg.ImportedTicket{
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
