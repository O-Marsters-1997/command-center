package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func (s *Store) autoHistories(ctx context.Context) (map[plan.TicketURL]plan.AutoHistory, error) {
	rows, err := s.q.AutoHistories(ctx)
	if err != nil {
		return nil, fmt.Errorf("select auto histories: %w", err)
	}
	out := make(map[plan.TicketURL]plan.AutoHistory, len(rows))
	for _, row := range rows {
		out[plan.TicketURL(row.TicketID)] = plan.AutoHistory{Rounds: int(row.Rounds), LastTip: plan.SHA(row.LastTip)}
	}
	return out, nil
}

// VerdictFacts measures checking time in successful ticks rather than wall time.
func (s *Store) VerdictFacts(ctx context.Context) (plan.VerdictFacts, error) {
	pushRows, err := s.latestPushes(ctx)
	if err != nil {
		return plan.VerdictFacts{}, fmt.Errorf("read latest pushes: %w", err)
	}
	checkingTicks, err := s.checkingTicks(ctx)
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
	memberships, err := s.launchMemberships(ctx)
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
	latest, err := s.LatestRowIntents(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read latest intents: %w", err)
	}
	repos, err := s.repoStatuses(ctx)
	if err != nil {
		return plan.Input{}, err
	}
	restacked, err := s.RestackedSinceLastPush(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read restacked tickets: %w", err)
	}
	lastVerdicts, err := s.lastVerdicts(ctx)
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

	passes, err := s.PassRuns(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read pass runs: %w", err)
	}

	auto, err := s.autoHistories(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read auto histories: %w", err)
	}

	verifies, err := s.latestVerifyRuns(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read verify runs: %w", err)
	}

	gauges, err := s.LatestReadings(ctx)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read latest readings: %w", err)
	}

	planTickets := make([]plan.Ticket, len(tickets))
	recorded, err := s.recordedEvents(ctx, tickets)
	if err != nil {
		return plan.Input{}, fmt.Errorf("read recorded events: %w", err)
	}
	verdicts := make(map[plan.TicketURL]string, len(lastVerdicts))
	for i, t := range tickets {
		planTickets[i] = t.Plan()
	}
	for url, v := range lastVerdicts {
		verdicts[plan.TicketURL(url)] = v
	}
	return plan.Input{
		Observed: observed, Tickets: planTickets, Obs: obs, Repos: repos,
		Memberships: memberships, Runs: runs, Pushes: pushes, Refreshes: refreshes,
		Restacked: restacked, Recorded: recorded, LastVerdicts: verdicts,
		Verdict: verdictFacts, Intents: intents, Latest: latest, Removals: removals,
		FiveHour: gauges[agentlog.FiveHour].Utilization,
		Passes:   passes,
		Auto:     auto,
		Verifies: verifies,
	}, nil
}

func (s *Store) recordedEvents(ctx context.Context, tickets []Ticket) (map[plan.TicketURL]plan.Recorded, error) {
	kinds, err := encodeJSON("recorded event kinds", []EventKind{EventPROpened, EventPRMerged, EventParked})
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRecordedEvents(ctx, kinds)
	if err != nil {
		return nil, fmt.Errorf("select recorded events: %w", err)
	}
	recorded := make(map[plan.TicketURL]plan.Recorded, len(tickets))
	for _, t := range tickets {
		recorded[plan.TicketURL(t.URL)] = plan.Recorded{}
	}
	for _, row := range rows {
		url := plan.TicketURL(row.TicketID.String)
		rec, ok := recorded[url]
		if !ok {
			continue
		}
		kind := EventKind(row.Kind)
		rec.Opened = rec.Opened || kind == EventPROpened
		rec.Merged = rec.Merged || kind == EventPRMerged
		rec.Parked = rec.Parked || kind == EventParked
		recorded[url] = rec
	}
	return recorded, nil
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
