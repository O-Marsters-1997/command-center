package view

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// importVerb is the intent verb whose pending rows mean a feature import is still queued. Its
// ticket id is the feature label.
const importVerb = "import"

// Candidate is one ticket the launch preview would start, as GET /launch/candidates serves it.
type Candidate struct {
	URL         string   `json:"url"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	Repo        string   `json:"repo"`
	Feature     string   `json:"feature"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
	Base        string   `json:"base"`
	BaseVerdict string   `json:"base_verdict"`
	PromptHash  string   `json:"prompt_hash"`
	BlockedBy   []string `json:"blocked_by"`
	Prompt      string   `json:"-"`
}

// LaunchModal is the launch modal fragment's view model. Exactly one of Pending, Refused and Empty
// explains a feature that has nothing to show yet.
type LaunchModal struct {
	Feature      string
	FeatureQuery string
	TicketQuery  string
	Pending      bool
	Refused      string
	Empty        bool
}

type candidateInputs struct {
	tickets []store.Ticket
	snap    plan.Snapshot
}

func (r *Reader) loadCandidateInputs(ctx context.Context, now time.Time) (candidateInputs, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return candidateInputs{}, err
	}
	snap, err := r.Snapshot(ctx, now)
	if err != nil {
		return candidateInputs{}, err
	}
	return candidateInputs{tickets: tickets, snap: snap}, nil
}

// Candidates previews what launching the selection in q would start: every ticket of ?feature=, or
// the repeated ?ticket= values. A request that selects nothing or something unlaunchable is an
// InvalidError.
func (r *Reader) Candidates(ctx context.Context, now time.Time, q url.Values) ([]Candidate, error) {
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return nil, err
	}
	requested, err := candidateSelection(q, in.tickets)
	if err != nil {
		return nil, InvalidError{err}
	}
	candidates, err := previewCandidates(requested, in)
	if err != nil {
		return nil, InvalidError{err}
	}
	return candidates, nil
}

func previewCandidates(requested []string, in candidateInputs) ([]Candidate, error) {
	rows, err := in.snap.Preview(requested)
	if err != nil {
		return nil, err
	}
	stored := make(map[string]store.Ticket, len(in.tickets))
	for _, t := range in.tickets {
		stored[t.URL] = t
	}
	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		t := stored[row.Ticket.URL]
		candidates = append(candidates, Candidate{
			URL: t.URL, Ref: ticketRef(t.URL), Title: t.Title, Repo: t.Repo, Feature: t.Feature,
			Label: row.Label.String(), Reason: string(row.Reason),
			Base:        "origin/" + row.Base,
			BaseVerdict: plan.VerdictLabel(row.BaseRun),
			PromptHash:  row.PromptHash, BlockedBy: t.BlockedBy, Prompt: row.Prompt,
		})
	}
	return candidates, nil
}

func candidateSelection(q url.Values, tickets []store.Ticket) ([]string, error) {
	if feature := q.Get("feature"); feature != "" {
		var selected []string
		for _, t := range tickets {
			if t.Feature == feature {
				selected = append(selected, t.URL)
			}
		}
		return selected, nil
	}
	requested := q["ticket"]
	if len(requested) == 0 {
		return nil, errors.New("either ?feature= or at least one ?ticket= is required")
	}
	return requested, nil
}

// FeatureModal is the launch modal for a whole feature: pending while its import is queued, then
// the last import's refusal or an empty notice when nothing was imported.
func (r *Reader) FeatureModal(ctx context.Context, now time.Time, feature string) (LaunchModal, error) {
	view := LaunchModal{Feature: feature, FeatureQuery: url.QueryEscape(feature)}

	intents, err := r.store.PendingVerbIntents(ctx, importVerb)
	if err != nil {
		return LaunchModal{}, err
	}
	for _, intent := range intents {
		if intent.TicketID == feature {
			view.Pending = true
			return view, nil
		}
	}

	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchModal{}, err
	}
	requested, err := candidateSelection(url.Values{"feature": {feature}}, in.tickets)
	if err != nil {
		return LaunchModal{}, err
	}
	candidates, err := previewCandidates(requested, in)
	if err != nil {
		return LaunchModal{}, err
	}

	if len(candidates) == 0 {
		lastErr, failed, err := r.store.LastImportError(ctx)
		if err != nil {
			return LaunchModal{}, err
		}
		if failed && lastErr.Feature == feature {
			view.Refused = lastErr.Message
		} else {
			view.Empty = true
		}
	}
	return view, nil
}

// TicketModal is the launch modal for the requested tickets, refused if the preview rejects them.
func (r *Reader) TicketModal(ctx context.Context, now time.Time, requested []string) (LaunchModal, error) {
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchModal{}, err
	}
	if _, err := previewCandidates(requested, in); err != nil {
		return LaunchModal{}, err
	}
	return LaunchModal{TicketQuery: candidateQuery(requested)}, nil
}

func candidateQuery(requested []string) string {
	q := make(url.Values, len(requested))
	for _, ticketURL := range requested {
		q.Add("ticket", ticketURL)
	}
	return q.Encode()
}
