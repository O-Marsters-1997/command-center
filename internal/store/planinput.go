package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// VerdictFacts measures checking time in successful ticks rather than wall time.
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
	intents, err := s.PendingIntents(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read pending intents: %w", err)
	}
	repos, err := s.repoStatuses(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	restacked, err := s.RestackedSinceLastPush(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read restacked tickets: %w", err)
	}
	lastVerdicts, err := s.LastVerdicts(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read last verdicts: %w", err)
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
	sessions := make(map[plan.TicketURL]string, len(runs))
	recorded := make(map[plan.TicketURL]plan.Recorded, len(tickets))
	verdicts := make(map[plan.TicketURL]string, len(lastVerdicts))
	for i, t := range tickets {
		planTickets[i] = t.Plan()
		url := plan.TicketURL(t.URL)
		if _, ok := runs[url]; ok {
			session, err := s.LatestSessionID(ctx, t.URL)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return plan.Input{}, fmt.Errorf("read latest session: %w", err)
			}
			if err == nil {
				sessions[url] = session
			}
		}
		var rec plan.Recorded
		if rec.Opened, err = s.HasEvent(ctx, t.URL, EventPROpened); err != nil {
			return plan.Input{}, fmt.Errorf("read pr_opened event: %w", err)
		}
		if rec.Merged, err = s.HasEvent(ctx, t.URL, EventPRMerged); err != nil {
			return plan.Input{}, fmt.Errorf("read pr_merged event: %w", err)
		}
		recorded[url] = rec
	}
	for url, v := range lastVerdicts {
		verdicts[plan.TicketURL(url)] = v
	}
	return plan.Input{
		Observed: observed, Tickets: planTickets, Obs: obs, Repos: repos,
		Memberships: memberships, Runs: runs, Pushes: pushes, Refreshes: refreshes,
		Restacked: restacked, Sessions: sessions, Recorded: recorded, LastVerdicts: verdicts,
		Verdict: verdictFacts, Intents: intents, Removals: removals,
		FiveHour: gauges[agentlog.FiveHour].Utilization,
	}, nil
}

func (s *Store) repoStatuses(ctx context.Context) ([]plan.RepoStatus, error) {
	repos, err := s.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("read repos: %w", err)
	}
	statuses := make([]plan.RepoStatus, len(repos))
	for i, r := range repos {
		statuses[i] = plan.RepoStatus{Name: r.Name, Phase: r.Phase, RefusalKind: r.RefusalKind, Refusal: r.Refusal}
	}
	return statuses, nil
}
