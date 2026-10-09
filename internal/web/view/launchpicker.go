package view

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// LaunchPicker is the /launch page: the features that have something to launch, each linking to
// its own launch dialog.
type LaunchPicker struct {
	Chrome
	Features []LaunchFeature
}

// LaunchFeature is one picker row. Now and OnUnlock count the feature's launchable tickets by the
// section the dialog would put them in.
type LaunchFeature struct {
	Name     string
	Path     string
	Now      int
	OnUnlock int
}

// LaunchPicker lists every feature with at least one launchable ticket, in name order.
func (r *Reader) LaunchPicker(ctx context.Context, now time.Time) (LaunchPicker, error) {
	chrome, err := r.Chrome(ctx, now, Params{})
	if err != nil {
		return LaunchPicker{}, err
	}
	in, err := r.loadCandidateInputs(ctx, now)
	if err != nil {
		return LaunchPicker{}, err
	}
	byFeature := map[string][]string{}
	for _, t := range in.tickets {
		if t.Feature != "" {
			byFeature[t.Feature] = append(byFeature[t.Feature], t.URL)
		}
	}
	page := LaunchPicker{Chrome: chrome}
	for feature, urls := range byFeature {
		candidates, err := previewCandidates(urls, in)
		if err != nil {
			return LaunchPicker{}, err
		}
		row := LaunchFeature{Name: feature, Path: "/f/" + url.PathEscape(feature) + "/launch"}
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
