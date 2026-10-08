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

// CheckStep is one line of a repo's four-step check list. Tone is one of the five tone words and
// Mark is the glyph shown beside Label.
type CheckStep struct {
	Label string
	Tone  string
	Mark  string
}

// Banner is the tracking state of one repo as the fragment GET /features/banner renders it.
type Banner struct {
	Repo      string
	State     string
	Tone      string
	Text      string
	BoardPath string
	Steps     []CheckStep
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
	b.Tone, b.Text = bannerWords(b.State, repo)
	b.Steps = checkSteps(b.State, repo.RefusalKind)
	if b.State == BannerReady {
		b.BoardPath = Params{Repo: repo.Name}.pagePath()
	}
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

func bannerWords(state string, repo store.Repo) (tone, text string) {
	switch state {
	case BannerCloning:
		return "live", "Checking the repo on GitHub, then cloning it."
	case BannerReady:
		if repo.SettingsSource == string(config.SourceFile) {
			return "done", "Settings from .command-centre.toml on main."
		}
		return "done", "No .command-centre.toml on main: defaults are in use."
	case BannerRefused:
		return "stop", repo.Refusal
	}
	return "idle", "Not tracked."
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
		step := CheckStep{Label: l.label, Tone: "idle", Mark: "-"}
		switch {
		case state == BannerReady:
			step.Tone, step.Mark = "done", "ok"
		case state == BannerCloning:
			step.Tone, step.Mark = "live", "..."
		case state == BannerRefused && rank(l.kind) < rank(refusalKind):
			step.Tone, step.Mark = "done", "ok"
		case state == BannerRefused && l.kind == refusalKind:
			step.Tone, step.Mark = "stop", "x"
		}
		steps = append(steps, step)
	}
	return steps
}
