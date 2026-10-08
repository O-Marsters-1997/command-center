package loop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ValidateFunc settles one tracked repo into ready or refused. An error means the answer is not
// known yet (gh unreachable), so the repo keeps its state and is tried again.
type ValidateFunc func(ctx context.Context, dataDir string, repo store.Repo, now time.Time) (store.Repo, error)

// RemoteFunc finds the clone URL of the GitHub repo named owner/name.
type RemoteFunc func(ctx context.Context, fullName string) (string, error)

// ValidateRepo checks repo on GitHub, ensures its checkout and reads its settings, ending ready
// with the settings read or refused with the kind that stopped it. The GitHub check runs first, so
// a repo it refuses is never cloned.
func ValidateRepo(ctx context.Context, dataDir string, repo store.Repo, now time.Time) (store.Repo, error) {
	kind, reason, err := git.CheckRepoOnGitHub(ctx, repo.Name)
	if err != nil {
		return repo, err
	}
	if kind != "" {
		return refuse(repo, kind, reason), nil
	}

	checkout := config.CheckoutPath(dataDir, repo.Name)
	if err := git.EnsureCheckout(ctx, repo.Name, repo.Remote, checkout); err != nil {
		return refuse(repo, plan.RefusalClone, err.Error()), nil
	}
	_, source, err := config.ReadRepoSettings(ctx, checkout)
	if err != nil {
		return refuse(repo, plan.RefusalSettingsParse, err.Error()), nil
	}

	repo.State, repo.RefusalKind, repo.Refusal = store.RepoReady, "", ""
	repo.SettingsSource, repo.SettingsReadAt = string(source), now
	return repo, nil
}

func refuse(repo store.Repo, kind, reason string) store.Repo {
	repo.State, repo.RefusalKind, repo.Refusal = store.RepoRefused, kind, reason
	return repo
}

// validateCloningRepos settles every repo still cloning. One repo's unknown answer is recorded
// as a tick error and does not hold up the others.
func (l *Loop) validateCloningRepos(ctx context.Context) error {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, repo := range repos {
		if repo.State != store.RepoCloning {
			continue
		}
		settled, err := l.validate(ctx, l.cfg.DataDir, repo, l.clock.Now())
		if err != nil {
			failures = append(failures, fmt.Errorf("validate %s: %w", repo.Name, err))
			continue
		}
		if err := l.store.SetRepoState(ctx, settled); err != nil {
			return err
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return l.recordTickError(ctx, errors.Join(failures...).Error())
}

func (l *Loop) recordTickError(ctx context.Context, message string) error {
	last, found, err := l.store.LastError(ctx)
	if err != nil {
		return err
	}
	if found && last.Message == message {
		return nil
	}
	return l.store.RecordTickError(ctx, store.TickError{At: l.clock.Now(), Message: message})
}

// applyTrackIntents puts each tracked repo into cloning, so the validation that follows in the
// same tick settles it. A ready repo is left alone; a refused one is validated afresh. A repo
// whose remote GitHub will not give is refused for Track again.
func (l *Loop) applyTrackIntents(ctx context.Context) error {
	return l.eachIntent(ctx, store.TrackVerb, func(intent store.VerbIntent) error {
		repos, err := l.store.Repos(ctx)
		if err != nil {
			return err
		}
		repo := store.Repo{Name: intent.TicketID, TrackedAt: l.clock.Now()}
		if i := slices.IndexFunc(repos, func(r store.Repo) bool { return strings.EqualFold(r.Name, repo.Name) }); i >= 0 {
			if repos[i].State == store.RepoReady {
				return nil
			}
			repo = repos[i]
		}
		if repo.Remote == "" {
			remote, err := l.remoteOf(ctx, repo.Name)
			if err != nil {
				return l.store.UpsertRepo(ctx, refuse(repo, plan.RefusalClone, err.Error()))
			}
			repo.Remote = remote
		}
		repo.State, repo.RefusalKind, repo.Refusal = store.RepoCloning, "", ""
		return l.store.UpsertRepo(ctx, repo)
	})
}
