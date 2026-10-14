package view

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type CheckStep struct {
	Label string
	Glyph plan.GlyphWord
}

// Banner is the tracking state of one repo as the fragment GET /repos/banner renders it.
type Banner struct {
	Repo  string
	State BannerState
	Glyph plan.GlyphWord
	Text  string
	Steps []CheckStep
}

// Polling reports whether the banner should poll for a state change.
func (b Banner) Polling() bool { return b.State == BannerCloning }

// CanTrack reports whether the banner offers a Track button.
func (b Banner) CanTrack() bool { return b.State == BannerUntracked || b.State == BannerRefused }

// TrackLabel is the Track button's text.
func (b Banner) TrackLabel() string {
	if b.State == BannerRefused {
		return "Track again"
	}
	return "Track"
}

// Banner shapes the tracking banner for scope. A pending Track reads as cloning before the loop
// has written the row.
func (r *Reader) Banner(ctx context.Context, scope string) (Banner, error) {
	pending, err := r.store.TrackPending(ctx, scope)
	if err != nil {
		return Banner{}, fmt.Errorf("track pending: %w", err)
	}
	repo, known, err := r.KnownRepo(ctx, scope)
	if err != nil {
		return Banner{}, fmt.Errorf("load known repo: %w", err)
	}
	b := Banner{Repo: scope}
	switch {
	case pending:
		b.State = BannerCloning
	case !known:
		b.State = BannerUntracked
	default:
		b.Repo = repo.Name
		b.State = bannerState(repo)
	}
	b.Glyph, b.Text = bannerWords(b.State, repo)
	b.Steps = checkSteps(b.State, repo.RefusalKind)
	return b, nil
}

func bannerState(repo store.Repo) BannerState {
	switch repo.Phase {
	case plan.RepoReady:
		return BannerReady
	case plan.RepoRefused:
		return BannerRefused
	case plan.RepoCloning, plan.RepoPhaseUnknown:
		return BannerCloning
	}
	return BannerCloning
}

func bannerWords(state BannerState, repo store.Repo) (glyph plan.GlyphWord, text string) {
	switch state {
	case BannerCloning:
		return plan.GlyphRunning, "Checking the repo on GitHub, then cloning it."
	case BannerReady:
		if repo.SettingsSource == string(config.SourceFile) {
			return plan.GlyphDone, "Settings from .command-centre.toml on main."
		}
		return plan.GlyphDone, "No .command-centre.toml on main: defaults are in use."
	case BannerRefused:
		return plan.GlyphFailed, repo.Refusal
	case BannerUnknown, BannerUntracked:
		return plan.GlyphReady, "Not tracked."
	}
	return plan.GlyphReady, "Not tracked."
}

// checkSteps lists the checks in display order. Validation runs squash-only, default branch,
// clone, settings file, so the refusal kind marks the first failure and the rest not reached.
func checkSteps(state BannerState, refusalKind plan.RefusalKind) []CheckStep {
	labels := []struct {
		label string
		kind  plan.RefusalKind
	}{
		{"clone", plan.RefusalClone},
		{"squash-only merges", plan.RefusalMergeSettings},
		{"default branch is main", plan.RefusalDefaultBranch},
		{"settings file", plan.RefusalSettingsParse},
	}
	order := []plan.RefusalKind{
		plan.RefusalMergeSettings, plan.RefusalDefaultBranch, plan.RefusalClone, plan.RefusalSettingsParse,
	}
	rank := func(kind plan.RefusalKind) int {
		for i, k := range order {
			if k == kind {
				return i
			}
		}
		return len(order)
	}
	steps := make([]CheckStep, 0, len(labels))
	for _, l := range labels {
		step := CheckStep{Label: l.label, Glyph: plan.GlyphReady}
		switch {
		case state == BannerReady:
			step.Glyph = plan.GlyphDone
		case state == BannerCloning:
			step.Glyph = plan.GlyphRunning
		case state == BannerRefused && rank(l.kind) < rank(refusalKind):
			step.Glyph = plan.GlyphDone
		case state == BannerRefused && l.kind == refusalKind:
			step.Glyph = plan.GlyphFailed
		}
		steps = append(steps, step)
	}
	return steps
}
