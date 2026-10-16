package loop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type ValidateFunc func(ctx context.Context, dataDir string, repo store.Repo, now time.Time) (store.Repo, error)

type RemoteFunc func(ctx context.Context, fullName string) (string, error)

func ValidateRepo(ctx context.Context, dataDir string, repo store.Repo, now time.Time) (store.Repo, error) {
	settings, err := gh.CLI{}.ReadRepoSettings(ctx, repo.Name)
	if err != nil {
		return repo, fmt.Errorf("check %s on github: %w", repo.Name, err)
	}
	kind, reason := plan.RepoRefusal(plan.MergeSettings(settings))
	if kind != plan.RefusalUnknown {
		return refuse(repo, kind, reason), nil
	}

	checkout := config.CheckoutPath(dataDir, repo.Name)
	if err := git.EnsureCheckout(ctx, repo.Name, repo.Remote, checkout); err != nil {
		return refuse(repo, plan.RefusalClone, err.Error()), nil
	}
	_, source, err := readRepoSettings(ctx, checkout)
	if err != nil {
		return refuse(repo, plan.RefusalSettingsParse, err.Error()), nil
	}

	repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoReady, plan.RefusalUnknown, ""
	repo.SettingsSource, repo.SettingsReadAt = string(source), now
	return repo, nil
}

func readRepoSettings(ctx context.Context, checkout string) (config.RepoSettings, config.Source, error) {
	found, err := git.HasFile(ctx, checkout, config.SettingsRef, config.SettingsFile)
	if err != nil {
		return config.RepoSettings{}, "", fmt.Errorf("look for %s on %s: %w", config.SettingsFile, config.SettingsRef, err)
	}
	var body []byte
	if found {
		text, err := git.ShowFile(ctx, checkout, config.SettingsRef, config.SettingsFile)
		if err != nil {
			return config.RepoSettings{}, "", fmt.Errorf("read %s on %s: %w", config.SettingsFile, config.SettingsRef, err)
		}
		body = []byte(text)
	}
	settings, source, err := config.ReadRepoSettings(body, found)
	if err != nil {
		return config.RepoSettings{}, "", fmt.Errorf("%s on %s: %w", config.SettingsFile, config.SettingsRef, err)
	}
	return settings, source, nil
}

func refuse(repo store.Repo, kind plan.RefusalKind, reason string) store.Repo {
	repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoRefused, kind, reason
	return repo
}

func (l *Loop) validateCloningRepos(ctx context.Context) error {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}
	var failures []error
	for _, repo := range repos {
		if repo.Phase != plan.RepoCloning {
			continue
		}
		settled, err := l.validate(ctx, l.cfg.DataDir, repo, l.clock.Now())
		if err != nil {
			failures = append(failures, fmt.Errorf("validate %s: %w", repo.Name, err))
			continue
		}
		if err := l.store.SetRepoState(ctx, settled); err != nil {
			return fmt.Errorf("set state of %s: %w", repo.Name, err)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return l.recordTickError(ctx, errors.Join(failures...).Error())
}

func (l *Loop) recordTickError(ctx context.Context, message string) error {
	last, err := l.store.LastError(ctx)
	if err != nil {
		return fmt.Errorf("last tick error: %w", err)
	}
	if last.IsSome() && last.Unwrap().Message == message {
		return nil
	}
	if err := l.store.RecordTickError(ctx, store.TickError{At: l.clock.Now(), Message: message}); err != nil {
		return fmt.Errorf("record tick error: %w", err)
	}
	return nil
}

func (l *Loop) applyTrackIntents(ctx context.Context) error {
	return l.eachPendingIntent(ctx, plan.VerbTrack, func(intent plan.Intent) error {
		repos, err := l.store.Repos(ctx)
		if err != nil {
			return fmt.Errorf("list repos: %w", err)
		}
		repo := store.Repo{Name: string(intent.Ticket), TrackedAt: l.clock.Now()}
		if i := slices.IndexFunc(repos, func(r store.Repo) bool { return strings.EqualFold(r.Name, repo.Name) }); i >= 0 {
			if repos[i].Phase == plan.RepoReady {
				return nil
			}
			repo = repos[i]
		}
		if repo.Remote == "" {
			remote, err := l.remoteOf(ctx, repo.Name)
			if err != nil {
				if upsertErr := l.store.UpsertRepo(ctx, refuse(repo, plan.RefusalClone, err.Error())); upsertErr != nil {
					return fmt.Errorf("refuse %s: %w", repo.Name, upsertErr)
				}
				return nil
			}
			repo.Remote = remote
		}
		repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoCloning, plan.RefusalUnknown, ""
		if err := l.store.UpsertRepo(ctx, repo); err != nil {
			return fmt.Errorf("start cloning %s: %w", repo.Name, err)
		}
		return nil
	})
}

func RepoNameForDir(ctx context.Context, dir string, repos []store.Repo) (name string, ok bool) {
	origin, err := git.OriginURL(ctx, dir)
	if err != nil {
		return "", false
	}
	for _, r := range repos {
		if git.SameRemote(origin, r.Remote) {
			return r.Name, true
		}
	}
	return "", false
}
