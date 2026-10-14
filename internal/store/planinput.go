package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// VerdictFacts reads each ticket's latest push and how long its verdict has been checking.
func (s *Store) VerdictFacts(ctx context.Context) (plan.VerdictFacts, error) {
	pushRows, err := s.LatestPushes(ctx)
	if err != nil {
		return plan.VerdictFacts{}, fmt.Errorf("read latest pushes: %w", err)
	}
	checkingTicks, err := s.CheckingTicks(ctx)
	if err != nil {
		return plan.VerdictFacts{}, fmt.Errorf("read checking ticks: %w", err)
	}
	checkingFor := make(map[plan.TicketURL]time.Duration, len(checkingTicks))
	for url, ticks := range checkingTicks {
		checkingFor[url] = time.Duration(ticks) * TickPeriod
	}
	return plan.VerdictFacts{PushRows: pushRows, CheckingFor: checkingFor}, nil
}

// PlanInput reads every durable fact and the last observation into a plan.Input.
// The caller sets Now.
func (s *Store) PlanInput(ctx context.Context) (plan.Input, error) {
	tickets, err := s.Tickets(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read tickets: %w", err)
	}
	obs, observed, err := s.LastObservation(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read last observation: %w", err)
	}
	memberships, err := s.LaunchMemberships(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read launch memberships: %w", err)
	}
	runs, err := s.LatestRunsByTicket(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read latest runs: %w", err)
	}
	pushes, err := s.PushFacts(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read push facts: %w", err)
	}
	refreshes, err := s.RefreshFacts(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read refresh facts: %w", err)
	}
	pendingVerbs, err := s.PendingIntentsByTicket(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read pending intents: %w", err)
	}
	removals, err := s.RemovalRefusals(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read removal refusals: %w", err)
	}
	verdictFacts, err := s.VerdictFacts(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read verdict facts: %w", err)
	}

	gauges, err := s.LatestReadings(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read latest readings: %w", err)
	}

	planTickets := make([]plan.Ticket, len(tickets))
	for i, t := range tickets {
		planTickets[i] = t.Plan()
	}
	return plan.Input{
		Observed: observed, Tickets: planTickets, Obs: obs,
		Memberships: memberships, Runs: runs, Pushes: pushes, Refreshes: refreshes,
		Verdict: verdictFacts, PendingVerbs: pendingVerbs, Removals: removals,
		FiveHour: gauges[agentlog.FiveHour].Utilization,
	}, nil
}
