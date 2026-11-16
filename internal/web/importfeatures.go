package web

import (
	"context"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func importOffers(
	ctx context.Context, repos []store.Repo, settings map[string]config.RepoSettings, resolve tracker.Resolver,
	tickets []store.Ticket,
) (map[string][]view.ImportOffer, error) {
	type repoFeature struct{ repo, feature string }
	imported := map[repoFeature]bool{}
	for _, t := range tickets {
		imported[repoFeature{t.Repo, t.Feature}] = true
	}

	offers := make(map[string][]view.ImportOffer, len(repos))
	var errs error
	for _, r := range repos {
		kind := config.DefaultRepoSettings().Tracker
		if s, ok := settings[r.Name]; ok {
			kind = s.Tracker
		}
		src, ok, err := tracker.ForRemote(resolve, tracker.Kind(kind), r.Remote)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("repo %s: %w", r.Name, err))
			continue
		}
		if !ok {
			continue
		}
		features, err := src.Features(ctx)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("list features for %s: %w", r.Name, err))
			continue
		}
		for _, f := range features {
			offer := view.ImportOffer{Feature: string(f), Imported: imported[repoFeature{r.Name, string(f)}]}
			if !offer.Imported {
				found, err := src.Tickets(ctx, string(f))
				if err != nil {
					errs = errors.Join(errs, fmt.Errorf("read %s tickets of %s: %w", f, r.Name, err))
					continue
				}
				offer.Tickets = len(found)
			}
			offers[r.Name] = append(offers[r.Name], offer)
		}
	}
	return offers, errs
}
