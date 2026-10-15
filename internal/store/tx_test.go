package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestReadsThatDecideAWriteRunOnTheTransactionsConnection(t *testing.T) {
	t.Parallel()

	s := cctest.Store(t)
	s.DB().SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	at := testNow

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
