package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type RepoState string

const (
	RepoCloning RepoState = "cloning"
	RepoReady   RepoState = "ready"
	RepoRefused RepoState = "refused"
)

type Repo struct {
	Name           string
	Remote         string
	State          RepoState
	RefusalKind    string
	Refusal        string
	SettingsSource string
	SettingsReadAt time.Time
	TrackedAt      time.Time
}

type RepoImport struct {
	ShortName string
	Repo      Repo
}

func (s *Store) Repos(ctx context.Context) ([]Repo, error) {
	rows, err := s.q.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("select repos: %w", err)
	}
	repos := make([]Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, Repo{
			Name: row.Name, Remote: row.Remote, State: RepoState(row.State),
			RefusalKind: row.RefusalKind.String, Refusal: row.Refusal.String,
			SettingsSource: row.SettingsSource.String, SettingsReadAt: row.SettingsReadAt.Time,
			TrackedAt: row.TrackedAt,
		})
	}
	return repos, nil
}

func (s *Store) UpsertRepo(ctx context.Context, r Repo) error {
	return upsertRepo(ctx, s.q, r)
}

func upsertRepo(ctx context.Context, q *ccdb.Queries, r Repo) error {
	err := q.UpsertRepo(ctx, ccdb.UpsertRepoParams{
		Name: r.Name, Remote: r.Remote, State: string(r.State),
		RefusalKind: nullIfEmpty(r.RefusalKind), Refusal: nullIfEmpty(r.Refusal),
		TrackedAt: r.TrackedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("upsert repo %s: %w", r.Name, err)
	}
	return nil
}

// SetRepoState writes r's state, refusal and settings read onto the row named r.Name.
func (s *Store) SetRepoState(ctx context.Context, r Repo) error {
	err := s.q.SetRepoState(ctx, ccdb.SetRepoStateParams{
		Name: r.Name, State: string(r.State),
		RefusalKind: nullIfEmpty(r.RefusalKind), Refusal: nullIfEmpty(r.Refusal),
		SettingsSource: nullIfEmpty(r.SettingsSource),
		SettingsReadAt: sql.NullTime{Time: r.SettingsReadAt.UTC(), Valid: !r.SettingsReadAt.IsZero()},
	})
	if err != nil {
		return fmt.Errorf("set repo %s state %s: %w", r.Name, r.State, err)
	}
	return nil
}

func (s *Store) ImportRepos(ctx context.Context, imports []RepoImport) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	qtx := s.q.WithTx(tx)
	for _, imp := range imports {
		if err = upsertRepo(ctx, qtx, imp.Repo); err != nil {
			return err
		}
		err = qtx.RenameTicketRepo(ctx, ccdb.RenameTicketRepoParams{NewName: imp.Repo.Name, OldName: imp.ShortName})
		if err != nil {
			return fmt.Errorf("rename tickets of %s to %s: %w", imp.ShortName, imp.Repo.Name, err)
		}
	}
	return tx.Commit()
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
