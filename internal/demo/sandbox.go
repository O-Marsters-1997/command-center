package demo

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// Sandbox is the real-git half of the demo world: one bare origin per repo, a checkout the loop
// works in, and a throwaway database. Close removes all of it.
type Sandbox struct {
	root     string
	DSN      string
	dropDB   func() error
	repos    []*sandboxRepo
	byOrigin map[string]*sandboxRepo
}

type sandboxRepo struct {
	scenarioName string
	stacking     bool
	compatCheck  string
	name         string
	origin       string
	checkout     string
	merger       string
}

// NewSandbox builds the repos' origins seeded from their files and a database.
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

	return sb, nil
}

func (s *Sandbox) addRepo(r Repo) (*sandboxRepo, error) {
	name := strings.ReplaceAll(r.Name, "/", "-")
	repo := &sandboxRepo{
		scenarioName: r.Name,
		stacking:     r.Stacking,
		compatCheck:  r.CompatCheck,
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

func (r *sandboxRepo) syncMain() error {
	for _, args := range [][]string{
		{"fetch", "-q", "origin"},
		{"checkout", "-q", "main"},
		{"reset", "-q", "--hard", "origin/main"},
	} {
		if _, err := git(r.merger, args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *sandboxRepo) squashMerge(branch, message string) error {
	if err := r.syncMain(); err != nil {
		return err
	}
	steps := [][]string{
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

// LandOnMain commits files to the named scenario repo's origin main, as a push that did not
// come from cc.
func (s *Sandbox) LandOnMain(repoName string, files map[string]string) error {
	idx := slices.IndexFunc(s.repos, func(r *sandboxRepo) bool { return r.scenarioName == repoName })
	if idx < 0 {
		return fmt.Errorf("no sandbox repo %q", repoName)
	}
	repo := s.repos[idx]
	if err := repo.syncMain(); err != nil {
		return err
	}
	if err := commitAll(repo.merger, "land on main", files); err != nil {
		return err
	}
	_, err := git(repo.merger, "push", "-q", "origin", "main")
	return err
}

// PushToBranch commits files to branch on the repo's origin, as a human pushing to a pull request.
func (s *Sandbox) PushToBranch(repo *sandboxRepo, branch string, files map[string]string) error {
	if _, err := git(repo.merger, "fetch", "-q", "origin"); err != nil {
		return err
	}
	if _, err := git(repo.merger, "checkout", "-q", "-B", branch, "origin/"+branch); err != nil {
		return err
	}
	if err := commitAll(repo.merger, "hand edit", files); err != nil {
		return err
	}
	_, err := git(repo.merger, "push", "-q", "origin", branch)
	return err
}

// Repos is the config.Repo for each scenario repo, pointed at its origin and sandbox checkout.
func (s *Sandbox) Repos(template config.Repo) []config.Repo {
	out := make([]config.Repo, 0, len(s.repos))
	for _, r := range s.repos {
		repo := template
		repo.Name = r.name
		repo.Remote = r.origin
		repo.Checkout = r.checkout
		repo.Stacking = r.stacking
		if r.compatCheck != "" {
			repo.Checks = verdict.Predicate{AllOf: []verdict.Predicate{{Success: ciCheck}, {Success: r.compatCheck}}}
			repo.CompatCheck = r.compatCheck
		}
		out = append(out, repo)
	}
	return out
}

// RunsDir is where the loop writes agent logs and prompts.
func (s *Sandbox) RunsDir() string { return filepath.Join(s.root, "runs") }

// Close drops the database and deletes the sandbox tree.
func (s *Sandbox) Close() error { return errors.Join(s.dropDB(), os.RemoveAll(s.root)) }

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
