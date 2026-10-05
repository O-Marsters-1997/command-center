package cc

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const pushableReposTTL = 60 * time.Second

// pushableReposCache holds gh.PushableRepos's own result for pushableReposTTL: a keystroke in the
// repos search box must not trigger a fresh paginated GitHub walk on every change event.
type pushableReposCache struct {
	fetch func(context.Context) ([]gh.RepoSummary, error)
	now   func() time.Time

	mu        sync.Mutex
	repos     []gh.RepoSummary
	fetchedAt time.Time
}

func newPushableReposCache(fetch func(context.Context) ([]gh.RepoSummary, error), now func() time.Time) *pushableReposCache {
	return &pushableReposCache{fetch: fetch, now: now}
}

// Get answers the last fetch taken within pushableReposTTL, fetching again once it has aged out.
func (c *pushableReposCache) Get(ctx context.Context) ([]gh.RepoSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fetchedAt.IsZero() || c.now().Sub(c.fetchedAt) >= pushableReposTTL {
		repos, err := c.fetch(ctx)
		if err != nil {
			return nil, err
		}
		c.repos, c.fetchedAt = repos, c.now()
	}
	return c.repos, nil
}

// repoRow is one row of the repos table: either a repo the app already knows, or a pushable
// GitHub match it does not track yet.
type repoRow struct {
	Name      string
	Tracked   bool
	State     string
	Tone      string
	ScopePath string
}

// reposTableView is the repos table's own fragment, rendered both inline on the full page and
// standalone by handleFeatureSearch.
type reposTableView struct {
	Repos []repoRow
}

// knownRepoRows is the repos page's default table: every configured repo, shown ready/done since
// this slice has no cloning or refused state yet -- that arrives once repos move into their own
// table.
func knownRepoRows(repos []Repo) []repoRow {
	rows := make([]repoRow, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, repoRow{Name: r.Name, Tracked: true, State: "ready", Tone: "done", ScopePath: repoScopePath(r.Name)})
	}
	return rows
}

// repoScopePath builds the scoped features page's own link. owner/name travels as a query value,
// never as a path segment, so its "/" needs no escaping scheme (plans/tracked-repos.md § Routes).
func repoScopePath(name string) string { return "/features?repo=" + name }

// filterPushable keeps the pushable repos whose full name contains q, case-insensitively.
func filterPushable(all []gh.RepoSummary, q string) []gh.RepoSummary {
	q = strings.ToLower(q)
	matches := make([]gh.RepoSummary, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.FullName), q) {
			matches = append(matches, r)
		}
	}
	return matches
}

// mergeRepoRows matches each pushable repo against the known list by remote, since a pushable
// match's own identity is GitHub's full name while a known repo's Name is whatever its [[repo]]
// block calls it.
func mergeRepoRows(known []Repo, pushable []gh.RepoSummary) []repoRow {
	rows := make([]repoRow, 0, len(pushable))
	for _, p := range pushable {
		if r, ok := knownRepoForRemote(known, p.SSHURL); ok {
			rows = append(rows, repoRow{Name: r.Name, Tracked: true, State: "ready", Tone: "done", ScopePath: repoScopePath(r.Name)})
			continue
		}
		rows = append(rows, repoRow{Name: p.FullName, Tracked: false, State: "not tracked", Tone: "idle", ScopePath: repoScopePath(p.FullName)})
	}
	return rows
}

func knownRepoForRemote(repos []Repo, remote string) (Repo, bool) {
	for _, r := range repos {
		if r.Remote != "" && sameRemote(r.Remote, remote) {
			return r, true
		}
	}
	return Repo{}, false
}

// repoNamed finds the configured repo named name, the identity the scoped features page's own
// ?repo= resolves against.
func repoNamed(repos []Repo, name string) (Repo, bool) {
	for _, r := range repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}
