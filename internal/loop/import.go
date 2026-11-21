package loop

import (
	"context"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyImportIntents(ctx context.Context) error {
	return l.eachPendingIntent(ctx, plan.VerbImport, func(intent plan.Intent) error {
		return l.importFeature(ctx, string(intent.Ticket), intent.Payload)
	})
}

func (l *Loop) importFeature(ctx context.Context, feature, onlyRepo string) error {
	matched, err := l.featureTickets(ctx, feature, onlyRepo)
	if err != nil {
		return err
	}

	err = l.store.ImportRepoTickets(ctx, feature, onlyRepo, matched, l.clock.Now())
	var conflict *store.FeatureConflictError
	var closure *store.FeatureClosureError
	switch {
	case errors.As(err, &conflict):
		err = l.store.RecordImportRefusal(ctx, feature, conflict.URL, conflict.Error(), l.clock.Now())
	case errors.As(err, &closure):
		err = l.store.RecordImportRefusal(ctx, feature, "", closure.Error(), l.clock.Now())
	}
	if err != nil {
		return fmt.Errorf("import tickets for %s: %w", feature, err)
	}
	return nil
}

func (l *Loop) featureTickets(ctx context.Context, feature, onlyRepo string) ([]store.ImportedTicket, error) {
	lastObs, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return nil, fmt.Errorf("last observation: %w", err)
	}
	repos, err := readyRepos(ctx, l.store)
	if err != nil {
		return nil, fmt.Errorf("list ready repos: %w", err)
	}
	var matched []store.ImportedTicket
	for _, repo := range repos {
		if onlyRepo != "" && repo.Name != onlyRepo {
			continue
		}
		trackerKind := config.DefaultRepoSettings().Tracker
		if settings, ok := lastObs.Settings[repo.Name]; ok {
			trackerKind = settings.Tracker
		}
		src, ok, err := tracker.ForRemote(l.trackerFor, tracker.Kind(trackerKind), repo.Remote)
		if err != nil {
			return nil, fmt.Errorf("import %s: %w", feature, err)
		}
		if !ok {
			continue
		}

		tickets, err := src.Tickets(ctx, feature)
		if err != nil {
			return nil, fmt.Errorf("import %s from %s: %w", feature, repo.Name, err)
		}
		for _, t := range tickets {
			matched = append(matched, store.ImportedTicket{Ticket: t, Repo: repo.Name, Source: trackerKind})
		}
	}
	return matched, nil
}

func (l *Loop) applyEditTicketIntents(ctx context.Context) error {
	intents, err := l.store.PendingEditTicketIntents(ctx)
	if err != nil {
		return fmt.Errorf("list pending edit-ticket intents: %w", err)
	}

	now := l.clock.Now()
	for _, intent := range intents {
		err := l.store.EditTicket(ctx, intent.TicketID, intent.Branch, intent.BlockedBy)
		var closure *store.FeatureClosureError
		switch {
		case errors.As(err, &closure):
			if err := l.store.RecordImportRefusal(ctx, closure.Feature, "", closure.Error(), now); err != nil {
				return fmt.Errorf("record import refusal for %s: %w", intent.TicketID, err)
			}
		case err != nil:
			return fmt.Errorf("edit %s: %w", intent.TicketID, err)
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return fmt.Errorf("consume intent %d for %s: %w", intent.ID, intent.TicketID, err)
		}
	}
	return nil
}
