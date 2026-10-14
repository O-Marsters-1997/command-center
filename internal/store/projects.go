package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

var (
	ErrProjectExists  = errors.New("project already exists")
	ErrInvalidProject = errors.New("invalid project")
	ErrNotFound       = errors.New("not found")
)

const (
	MinHue = 1
	MaxHue = 3

	uniqueViolation = "23505"
)

var ProjectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)

type Project struct {
	Name  string
	Hue   int
	Repos []string
}

func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.Projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("select projects: %w", err)
	}
	repos, err := s.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("select repos: %w", err)
	}
	projects := make([]Project, 0, len(rows))
	index := make(map[string]int, len(rows))
	for _, row := range rows {
		index[row.Name] = len(projects)
		projects = append(projects, Project{Name: row.Name, Hue: int(row.Hue)})
	}
	for _, repo := range repos {
		if i, ok := index[repo.Project]; ok {
			projects[i].Repos = append(projects[i].Repos, repo.Name)
		}
	}
	return projects, nil
}

func (s *Store) CreateProject(ctx context.Context, p NewProject) (err error) {
	name := p.Name
	if !ProjectNamePattern.MatchString(name) || p.Hue < MinHue || p.Hue > MaxHue {
		return fmt.Errorf("create project %q with hue %d: %w", name, p.Hue, ErrInvalidProject)
	}
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
	id, err := qtx.CreateProject(ctx, ccdb.CreateProjectParams{Name: name, Hue: int16(p.Hue), CreatedAt: p.At.UTC()})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return fmt.Errorf("create project %q: %w", name, ErrProjectExists)
	}
	if err != nil {
		return fmt.Errorf("create project %q: %w", name, err)
	}
	for _, repo := range p.Repos {
		if err = setRepoProjectID(ctx, qtx, repo, sql.NullInt64{Int64: id, Valid: true}); err != nil {
			return fmt.Errorf("assign repo %s to project %q: %w", repo, name, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit project %q: %w", name, err)
	}
	return nil
}

func (s *Store) SetRepoProject(ctx context.Context, repo, project string) error {
	var id sql.NullInt64
	if project != "" {
		projectID, err := s.q.ProjectID(ctx, project)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("project %q: %w", project, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("select project %q: %w", project, err)
		}
		id = sql.NullInt64{Int64: projectID, Valid: true}
	}
	if err := setRepoProjectID(ctx, s.q, repo, id); err != nil {
		return fmt.Errorf("set project of repo %s: %w", repo, err)
	}
	return nil
}

func setRepoProjectID(ctx context.Context, q *ccdb.Queries, repo string, id sql.NullInt64) error {
	n, err := q.SetRepoProjectID(ctx, ccdb.SetRepoProjectIDParams{Name: repo, ProjectID: id})
	if err != nil {
		return fmt.Errorf("set project of repo %s: %w", repo, err)
	}
	if n == 0 {
		return fmt.Errorf("repo %q: %w", repo, ErrNotFound)
	}
	return nil
}

func (s *Store) EditProject(ctx context.Context, name, newName string, hue int) error {
	if !ProjectNamePattern.MatchString(newName) || hue < MinHue || hue > MaxHue {
		return fmt.Errorf("edit project %q to %q with hue %d: %w", name, newName, hue, ErrInvalidProject)
	}
	n, err := s.q.EditProject(ctx, ccdb.EditProjectParams{OldName: name, NewName: newName, Hue: int16(hue)})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return fmt.Errorf("edit project %q to %q: %w", name, newName, ErrProjectExists)
	}
	if err != nil {
		return fmt.Errorf("edit project %q: %w", name, err)
	}
	if n == 0 {
		return fmt.Errorf("project %q: %w", name, ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteProject(ctx context.Context, name string) error {
	n, err := s.q.DeleteProject(ctx, name)
	if err != nil {
		return fmt.Errorf("delete project %q: %w", name, err)
	}
	if n == 0 {
		return fmt.Errorf("project %q: %w", name, ErrNotFound)
	}
	return nil
}
