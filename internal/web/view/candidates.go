package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

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

type LaunchModal struct {
	Feature      string
	FeatureQuery string
	TicketQuery  string
	Pending      bool
	Refused      string
	Empty        bool
}

type LaunchPicker struct {
	Chrome
	Features []LaunchFeature
	Launch   *LaunchModal
}

type LaunchFeature struct {
	Name     string
	Path     string
	Now      int
	OnUnlock int
}

func (r *Reader) LaunchPicker(ctx context.Context, now time.Time) (LaunchPicker, error) {
	chrome, err := r.Chrome(ctx, now, Params{})
	if err != nil {
		return LaunchPicker{}, fmt.Errorf("load chrome: %w", err)
	}
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchPicker{}, fmt.Errorf("load candidate inputs: %w", err)
	}
	byFeature := map[string][]string{}
	for _, t := range in.tickets {
		if t.Feature != "" {
			byFeature[t.Feature] = append(byFeature[t.Feature], t.URL)
		}
	}
	chrome.titled("Launch")
	chrome.Crumbs = []Crumb{{Label: "Launch a feature"}}
	page := LaunchPicker{Chrome: chrome}
	for feature, urls := range byFeature {
		candidates, err := previewCandidates(urls, in)
		if err != nil {
			return LaunchPicker{}, fmt.Errorf("preview launch of feature %s: %w", feature, err)
		}
		row := LaunchFeature{Name: feature, Path: featureLaunchPath(feature)}
		for _, c := range candidates {
			switch c.Label {
			case plan.Now.String():
				row.Now++
			case plan.OnUnlock.String():
				row.OnUnlock++
			}
		}
		if row.Now+row.OnUnlock > 0 {
			page.Features = append(page.Features, row)
		}
	}
	slices.SortFunc(page.Features, func(a, b LaunchFeature) int { return cmp.Compare(a.Name, b.Name) })
	return page, nil
}

func (m LaunchModal) Path() string {
	if m.Feature != "" {
		return featureLaunchPath(m.Feature)
	}
	return "/launch?" + m.TicketQuery
}

func featureLaunchPath(feature string) string {
	return "/f/" + url.PathEscape(feature) + "/launch"
}

type candidateInputs struct {
	tickets []store.Ticket
	snap    plan.Snapshot
}

func (r *Reader) loadCandidateInputs(ctx context.Context, now time.Time) (candidateInputs, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return candidateInputs{}, fmt.Errorf("load tickets: %w", err)
	}
	snap, err := r.Snapshot(ctx, now)
	if err != nil {
		return candidateInputs{}, fmt.Errorf("load snapshot: %w", err)
	}
	return candidateInputs{tickets: tickets, snap: snap}, nil
}

func (r *Reader) Candidates(ctx context.Context, now time.Time, q url.Values) ([]Candidate, error) {
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load candidate inputs: %w", err)
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
	rows, err := in.snap.Preview(plan.TicketURLs(requested))
	if err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	stored := make(map[string]store.Ticket, len(in.tickets))
	for _, t := range in.tickets {
		stored[t.URL] = t
	}
	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		t := stored[string(row.Ticket.URL)]
		candidates = append(candidates, Candidate{
			URL: t.URL, Ref: ticketRef(t.URL), Title: t.Title, Repo: t.Repo, Feature: t.Feature,
			Label: row.Label.String(), Reason: string(row.Reason),
			Base:        "origin/" + row.Base,
			BaseVerdict: runVerdictLabel(row.BaseRun),
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

func (r *Reader) FeatureModal(ctx context.Context, now time.Time, feature string) (LaunchModal, error) {
	view := LaunchModal{Feature: feature, FeatureQuery: url.QueryEscape(feature)}

	intents, err := r.store.PendingVerbIntents(ctx, plan.VerbImport)
	if err != nil {
		return LaunchModal{}, fmt.Errorf("load pending verb intents: %w", err)
	}
	for _, intent := range intents {
		if intent.TicketID == feature {
			view.Pending = true
			return view, nil
		}
	}

	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchModal{}, fmt.Errorf("load candidate inputs: %w", err)
	}
	requested, err := candidateSelection(url.Values{"feature": {feature}}, in.tickets)
	if err != nil {
		return LaunchModal{}, fmt.Errorf("load candidate selection: %w", err)
	}
	candidates, err := previewCandidates(requested, in)
	if err != nil {
		return LaunchModal{}, fmt.Errorf("preview candidates: %w", err)
	}

	if len(candidates) == 0 {
		lastErr, failed, err := r.store.LastImportError(ctx)
		if err != nil {
			return LaunchModal{}, fmt.Errorf("load last import error: %w", err)
		}
		if failed && lastErr.Feature == feature {
			view.Refused = lastErr.Message
		} else {
			view.Empty = true
		}
	}
	return view, nil
}

func (r *Reader) TicketModal(ctx context.Context, now time.Time, requested []string) (LaunchModal, error) {
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchModal{}, fmt.Errorf("load candidate inputs: %w", err)
	}
	if _, err := previewCandidates(requested, in); err != nil {
		return LaunchModal{}, fmt.Errorf("preview candidates: %w", err)
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
