package cc

import (
	"context"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// VerdictFacts reads the store facts the CI verdict needs beyond the observation: each ticket's
// latest push and how long its verdict has been checking.
func (s *Store) VerdictFacts(ctx context.Context) (plan.VerdictFacts, error) {
	pushRows, err := s.LatestPushes(ctx)
	if err != nil {
		return plan.VerdictFacts{}, err
	}
	checkingTicks, err := s.CheckingTicks(ctx)
	if err != nil {
		return plan.VerdictFacts{}, err
	}
	checkingFor := make(map[string]time.Duration, len(checkingTicks))
	for url, ticks := range checkingTicks {
		checkingFor[url] = time.Duration(ticks) * tickPeriod
	}
	return plan.VerdictFacts{PushRows: pushRows, CheckingFor: checkingFor}, nil
}

// PlanInput reads every durable fact and the last observation into the plan.Input one
// derivation needs. The caller sets Now.
func (s *Store) PlanInput(ctx context.Context) (plan.Input, error) {
	tickets, err := s.Tickets(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	obs, observed, err := s.LastObservation(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	memberships, err := s.LaunchMemberships(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	runs, err := s.LatestRunsByTicket(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	pushes, err := s.PushFacts(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	refreshes, err := s.RefreshFacts(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	pendingVerbs, err := s.PendingIntentsByTicket(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	removals, err := s.RemovalRefusals(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	verdictFacts, err := s.VerdictFacts(ctx)
	if err != nil {
		return plan.Input{}, err
	}

	gauges, err := s.LatestReadings(ctx)
	if err != nil {
		return plan.Input{}, err
	}

	planTickets := make([]plan.Ticket, len(tickets))
	for i, t := range tickets {
		planTickets[i] = planTicket(t)
	}
	return plan.Input{
		Observed: observed, Tickets: planTickets, Obs: obs,
		Memberships: memberships, Runs: runs, Pushes: pushes, Refreshes: refreshes,
		Verdict: verdictFacts, PendingVerbs: pendingVerbs, Removals: removals,
		FiveHour: gauges[agentlog.FiveHour].Utilization,
	}, nil
}
