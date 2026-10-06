package demo

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
)

const fakeTpScript = `#!/bin/sh
set -eu
branch="$2"
base="$4"
path="$(cd "$(dirname "$PWD")" && pwd)/wt-$branch"
git worktree add -b "$branch" "$path" "$base" >&2
printf '%s\n' "$path"
`

// Sandbox is the real-git half of the demo world: one bare origin per repo, a checkout the loop
// works in, a throwaway database, and a fake tp on PATH. Close removes all of it.
type Sandbox struct {
	root     string
	DSN      string
	dropDB   func() error
	oldPath  string
	repos    []*sandboxRepo
	byOrigin map[string]*sandboxRepo
}

type sandboxRepo struct {
	scenarioName string
	name         string
	origin       string
	checkout     string
	merger       string
}

// NewSandbox builds the repos' origins seeded from their files and a database, and puts the fake
// tp first on PATH until Close.
func NewSandbox(repos []Repo) (_ *Sandbox, err error) {
	root, err := os.MkdirTemp("", "cc-demo-")
	if err != nil {
		return nil, err
	}
	dsn, dropDB, err := cctest.Create()
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(root))
	}
	sb := &Sandbox{root: root, DSN: dsn, dropDB: dropDB, byOrigin: map[string]*sandboxRepo{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, sb.Close())
		}
	}()

	for _, r := range repos {
		repo, err := sb.addRepo(r)
		if err != nil {
			return nil, err
		}
		sb.repos = append(sb.repos, repo)
		sb.byOrigin[repo.origin] = repo
	}

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(fakeTpScript), 0o700); err != nil { //nolint:gosec // an executable script
		return nil, err
	}
	sb.oldPath = os.Getenv("PATH")
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+sb.oldPath); err != nil {
		return nil, err
	}
	return sb, nil
}

func (s *Sandbox) addRepo(r Repo) (*sandboxRepo, error) {
	name := strings.ReplaceAll(r.Name, "/", "-")
	repo := &sandboxRepo{
		scenarioName: r.Name,
		name:         name,
		origin:       filepath.Join(s.root, "origins", filepath.FromSlash(r.Name)+".git"),
		checkout:     filepath.Join(s.root, "repos", name),
		merger:       filepath.Join(s.root, "mergers", name),
	}
	if err := os.MkdirAll(repo.origin, 0o750); err != nil {
		return nil, err
	}
	if _, err := git(repo.origin, "init", "-q", "--bare", "-b", "main"); err != nil {
		return nil, err
	}
	if _, err := git(s.root, "clone", "-q", repo.origin, repo.merger); err != nil {
		return nil, err
	}
	if _, err := git(repo.merger, "checkout", "-q", "-b", "main"); err != nil {
		return nil, err
	}
	if err := commitAll(repo.merger, "seed "+path.Base(r.Name), r.Files); err != nil {
		return nil, err
	}
	if _, err := git(repo.merger, "push", "-q", "origin", "main"); err != nil {
		return nil, err
	}
	return repo, nil
}

func (r *sandboxRepo) squashMerge(branch, message string) error {
	steps := [][]string{
		{"fetch", "-q", "origin"},
		{"checkout", "-q", "main"},
		{"reset", "-q", "--hard", "origin/main"},
		{"merge", "-q", "--squash", "origin/" + branch},
		{"commit", "-q", "-m", message},
		{"push", "-q", "origin", "main"},
	}
	for _, args := range steps {
		if _, err := git(r.merger, args...); err != nil {
			return err
		}
	}
	return nil
}

// Repos is the cc.Repo for each scenario repo, pointed at its origin and sandbox checkout.
func (s *Sandbox) Repos(template cc.Repo) []cc.Repo {
	out := make([]cc.Repo, 0, len(s.repos))
	for _, r := range s.repos {
		repo := template
		repo.Name = r.name
		repo.Remote = r.origin
		repo.Checkout = r.checkout
		out = append(out, repo)
	}
	return out
}

// RunsDir is where the loop writes agent logs and prompts.
func (s *Sandbox) RunsDir() string { return filepath.Join(s.root, "runs") }

// Close drops the database, restores PATH and deletes the sandbox tree.
func (s *Sandbox) Close() error {
	var errs []error
	if s.oldPath != "" {
		errs = append(errs, os.Setenv("PATH", s.oldPath))
	}
	errs = append(errs, s.dropDB(), os.RemoveAll(s.root))
	return errors.Join(errs...)
}

func (s *Sandbox) repoFor(gitDir string) (*sandboxRepo, error) {
	origin, err := git(gitDir, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	repo, ok := s.byOrigin[origin]
	if !ok {
		return nil, fmt.Errorf("no sandbox repo has origin %s", origin)
	}
	return repo, nil
}
