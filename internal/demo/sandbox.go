package demo

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

var errMergeConflict = errors.New("branch conflicts with main")

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
	compatCheck  string
	origin       string
	checkout     string
	merger       string
}

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
		compatCheck:  r.CompatCheck,
		origin:       filepath.Join(s.root, "origins", filepath.FromSlash(r.Name)+".git"),
		checkout:     config.CheckoutPath(s.root, r.Name),
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
	seed := maps.Clone(r.Files)
	if seed == nil {
		seed = map[string]string{}
	}
	seed[config.SettingsFile] = settingsBody(r)
	if err := commitAll(repo.merger, "seed "+path.Base(r.Name), seed); err != nil {
		return nil, err
	}
	if _, err := git(repo.merger, "push", "-q", "origin", "main"); err != nil {
		return nil, err
	}
	return repo, nil
}

func settingsBody(r Repo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stacking = %t\n", r.Stacking)
	if r.CompatCheck == "" {
		fmt.Fprintf(&b, "\n[checks]\nsuccess = %q\n", ciCheck)
		return b.String()
	}
	fmt.Fprintf(&b, "compat_check = %q\n\n[checks]\nall_of = [{ success = %q }, { success = %q }]\n",
		r.CompatCheck, ciCheck, r.CompatCheck)
	return b.String()
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
	if _, err := git(r.merger, "merge", "-q", "--squash", "origin/"+branch); err != nil {
		_, resetErr := git(r.merger, "reset", "-q", "--hard")
		return errors.Join(errMergeConflict, err, resetErr)
	}
	for _, args := range [][]string{{"commit", "-q", "-m", message}, {"push", "-q", "origin", "main"}} {
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

func (s *Sandbox) Repos(trackedAt time.Time) []store.Repo {
	out := make([]store.Repo, 0, len(s.repos))
	for _, r := range s.repos {
		out = append(out, store.Repo{Name: r.scenarioName, Remote: r.origin, State: store.RepoReady, TrackedAt: trackedAt})
	}
	return out
}

func (s *Sandbox) RunsDir() string { return filepath.Join(s.root, "runs") }

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

const (
	authorName  = "demo"
	authorEmail = "demo@example.com"
)

func git(dir string, args ...string) (string, error) {
	full := append([]string{
		"-C", dir,
		"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail,
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func writeFiles(dir string, files map[string]string) error {
	for name, contents := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func commitAll(dir, message string, files map[string]string) error {
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	if _, err := git(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := git(dir, "commit", "-q", "--allow-empty", "-m", message)
	return err
}
