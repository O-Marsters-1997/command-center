package view

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type LaunchPicker struct {
	Chrome
	Features []LaunchFeature
	Launch   *LaunchModal // set on /launch?ticket=…
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
