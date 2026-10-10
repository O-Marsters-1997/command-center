package view

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// Banner states, as the page's polling banner reports them back in its seen parameter.
const (
	BannerUntracked = "untracked"
	BannerCloning   = "cloning"
	BannerReady     = "ready"
	BannerRefused   = "refused"
)

// CheckStep is one line of a repo's four-step check list. Glyph is one of the eight glyph words and
// Mark is the glyph shown beside Label.
type CheckStep struct {
	Label string
	Glyph string
	Mark  string
}

// Banner is the tracking state of one repo as the fragment GET /repos/banner renders it.
type Banner struct {
	Repo  string
	State string
	Glyph string
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
		return Banner{}, err
	}
	repo, known, err := r.KnownRepo(ctx, scope)
	if err != nil {
		return Banner{}, err
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

func bannerState(repo store.Repo) string {
	switch repo.State {
	case store.RepoReady:
		return BannerReady
	case store.RepoRefused:
		return BannerRefused
	case store.RepoCloning:
	}
	return BannerCloning
}

func bannerWords(state string, repo store.Repo) (glyph, text string) {
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
	}
	return plan.GlyphReady, "Not tracked."
}

// checkSteps lists the checks in display order. Validation runs squash-only, default branch,
// clone, settings file, so the refusal kind marks the first failure and the rest not reached.
func checkSteps(state, refusalKind string) []CheckStep {
	labels := []struct{ label, kind string }{
		{"clone", plan.RefusalClone},
		{"squash-only merges", plan.RefusalMergeSettings},
		{"default branch is main", plan.RefusalDefaultBranch},
		{"settings file", plan.RefusalSettingsParse},
	}
	order := []string{plan.RefusalMergeSettings, plan.RefusalDefaultBranch, plan.RefusalClone, plan.RefusalSettingsParse}
	rank := func(kind string) int {
		for i, k := range order {
			if k == kind {
				return i
			}
		}
		return len(order)
	}
	steps := make([]CheckStep, 0, len(labels))
	for _, l := range labels {
		step := CheckStep{Label: l.label, Glyph: plan.GlyphReady, Mark: "-"}
		switch {
		case state == BannerReady:
			step.Glyph, step.Mark = plan.GlyphDone, "ok"
		case state == BannerCloning:
			step.Glyph, step.Mark = plan.GlyphRunning, "..."
		case state == BannerRefused && rank(l.kind) < rank(refusalKind):
			step.Glyph, step.Mark = plan.GlyphDone, "ok"
		case state == BannerRefused && l.kind == refusalKind:
			step.Glyph, step.Mark = plan.GlyphFailed, "x"
		}
		steps = append(steps, step)
	}
	return steps
}
