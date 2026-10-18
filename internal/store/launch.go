package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

const maxLaunchMemberships = 5000

type launchPayload struct {
	PromptHash string           `json:"prompt_hash"`
	Group      string           `json:"group"`
	Context    string           `json:"context"`
	Choice     plan.AgentChoice `json:"choice"`
	Workflow   string           `json:"workflow"`
}

func choiceColumns(c plan.AgentChoice) (model, effort sql.NullString) {
	model = nullIfEmpty(c.Model)
	if c.Effort.IsSome() {
		effort = notNull(string(c.Effort.Unwrap()))
	}
	return model, effort
}

func choiceFrom(model, effort sql.NullString) plan.AgentChoice {
	choice := plan.AgentChoice{Model: model.String, Effort: option.None[plan.Effort]()}
	if effort.Valid {
		choice.Effort = option.Some(plan.Effort(effort.String))
	}
	return choice
}

func (s *Store) QueueLaunchIntent(ctx context.Context, l LaunchRequest) error {
	payload, err := json.Marshal(launchPayload{
		PromptHash: l.PromptHash, Group: l.Group, Context: l.Context, Choice: l.Choice, Workflow: l.Workflow,
	})
	if err != nil {
		return fmt.Errorf("encode launch intent payload for %s: %w", l.Ticket, err)
	}
	err = s.q.QueueLaunchIntent(ctx, ccdb.QueueLaunchIntentParams{
		At:       l.At.UTC(),
		TicketID: l.Ticket,
		Payload:  payload,
	})
	if err != nil {
		return fmt.Errorf("queue launch intent for %s: %w", l.Ticket, err)
	}
	return nil
}

// ApplyLaunchIntents groups unconsumed launch intents and marks them consumed atomically.
func (s *Store) ApplyLaunchIntents(ctx context.Context, now time.Time) error {
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		nowUTC := now.UTC()
		groups, order, err := claimLaunchIntents(ctx, q, nowUTC)
		if err != nil {
			return err
		}

		for _, group := range order {
			if err := insertLaunch(ctx, q, nowUTC, groups[group]); err != nil {
				return fmt.Errorf("apply launch group %s: %w", group, err)
			}
		}
		return nil
	})
}

type pendingIntent struct {
	ticketID   string
	promptHash string
	context    string
	choice     plan.AgentChoice
	workflow   string
}

func claimLaunchIntents(
	ctx context.Context, q *ccdb.Queries, at time.Time,
) (map[string][]pendingIntent, []string, error) {
	rows, err := q.ClaimLaunchIntents(ctx, notNullTime(at))
	if err != nil {
		return nil, nil, fmt.Errorf("claim launch intents: %w", err)
	}
	slices.SortFunc(rows, func(a, b ccdb.ClaimLaunchIntentsRow) int { return cmp.Compare(a.ID, b.ID) })

	groups := map[string][]pendingIntent{}
	var order []string
	for _, row := range rows {
		var payload launchPayload
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode launch intent payload for %s: %w", row.TicketID, err)
		}
		if _, seen := groups[payload.Group]; !seen {
			order = append(order, payload.Group)
		}
		groups[payload.Group] = append(groups[payload.Group],
			pendingIntent{
				ticketID: row.TicketID, promptHash: payload.PromptHash,
				context: payload.Context, choice: payload.Choice, workflow: payload.Workflow,
			})
	}
	return groups, order, nil
}

func insertLaunch(ctx context.Context, q *ccdb.Queries, at time.Time, members []pendingIntent) error {
	model, effort := choiceColumns(members[0].choice)
	launchID, err := q.InsertLaunch(ctx, ccdb.InsertLaunchParams{
		CreatedAt: at, Context: members[0].context, Model: model, Effort: effort, Workflow: members[0].workflow,
	})
	if err != nil {
		return fmt.Errorf("insert launch: %w", err)
	}

	type memberRow struct {
		TicketID   string `json:"ticket_id"`
		PromptHash string `json:"prompt_hash"`
	}
	rows := make([]memberRow, len(members))
	for i, m := range members {
		rows[i] = memberRow{TicketID: m.ticketID, PromptHash: m.promptHash}
	}
	encodedMembers, err := encodeJSON("launch members", rows)
	if err != nil {
		return err
	}
	if err := q.InsertLaunchMembers(ctx, ccdb.InsertLaunchMembersParams{
		LaunchID: launchID, Members: encodedMembers,
	}); err != nil {
		return fmt.Errorf("insert %d launch members: %w", len(members), err)
	}

	detail := fmt.Sprintf("launch %d authorised with %d member(s)", launchID, len(members))
	if err := q.InsertLaunchEvent(ctx, ccdb.InsertLaunchEventParams{At: at, Detail: detail}); err != nil {
		return fmt.Errorf("append launch event: %w", err)
	}
	return nil
}

// LaunchMemberships includes cancelled tickets until they are relaunched.
func (s *Store) LaunchMemberships(ctx context.Context) (map[plan.TicketURL]plan.LaunchMembership, error) {
	rows, err := s.q.ListLaunchMemberships(ctx, maxLaunchMemberships)
	if err != nil {
		return nil, fmt.Errorf("select launch memberships: %w", err)
	}

	memberships := map[plan.TicketURL]plan.LaunchMembership{}
	cancelled := map[plan.TicketURL]bool{}
	for _, row := range rows {
		if row.State == "cancelled" {
			cancelled[plan.TicketURL(row.TicketID)] = true
			continue
		}
		memberships[plan.TicketURL(row.TicketID)] = plan.LaunchMembership{
			LaunchID:   row.LaunchID,
			Members:    int(row.Members),
			PromptHash: row.PromptHash,
			Context:    row.Context,
			Choice:     choiceFrom(row.Model, row.Effort),
			Workflow:   row.Workflow,
		}
	}
	for ticketID := range cancelled {
		if _, active := memberships[ticketID]; !active {
			memberships[ticketID] = plan.LaunchMembership{Cancelled: true}
		}
	}
	return memberships, nil
}

// CancelLaunchesFor returns the number of memberships withdrawn across active launches.
func (s *Store) CancelLaunchesFor(ctx context.Context, ticketID string) (int, error) {
	var members int
	err := s.inTx(ctx, func(q *ccdb.Queries) error {
		count, err := q.ActiveLaunchMemberCount(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("count members of active launches for %s: %w", ticketID, err)
		}
		if _, err := q.CancelActiveLaunches(ctx, ticketID); err != nil {
			return fmt.Errorf("cancel launches for %s: %w", ticketID, err)
		}
		members = int(count)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return members, nil
}

func (s *Store) FeatureFirstLaunch(ctx context.Context, feature string) (time.Time, error) {
	at, err := s.q.FeatureFirstLaunch(ctx, feature)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("launch of feature %q: %w", feature, ErrNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("select first launch of feature %q: %w", feature, err)
	}
	return at, nil
}
