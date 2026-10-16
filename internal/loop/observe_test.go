package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

type observerFixture struct {
	repoPath string
	store    *storepkg.Store
	observe  loop.ObserveFunc
}

func newObserverFixture(t *testing.T) *observerFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	cfg, _ := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t)
	trackRepo(t, store, "repo", filepath.Join(root, "remote.git"))
	return &observerFixture{repoPath: repoPath, store: store, observe: loop.NewObserver(store, &ghtest.Forge{}, cfg)}
}

func (f *observerFixture) observation(t *testing.T) plan.Observation {
	t.Helper()
	obs, err := f.observe(t.Context())
	if err != nil {
		t.Fatalf("observe() error = %v", err)
	}
	return obs
}

func (f *observerFixture) pushBranch(t *testing.T, branch, file, body string) {
	t.Helper()
	gittest.Run(t, f.repoPath, "switch", "-q", "-C", branch, "origin/main")
	gittest.Commit(t, f.repoPath, file, body)
	gittest.Run(t, f.repoPath, "push", "-q", "-f", "origin", branch)
	gittest.Run(t, f.repoPath, "switch", "-q", "main")
}

func TestNewObserver(t *testing.T) {
	t.Parallel()

	t.Run("reads settings from origin/main each tick and never from a branch", func(t *testing.T) {
		t.Parallel()
		f := newObserverFixture(t)
		deny := func() []string { return f.observation(t).Settings["repo"].Deny }

		if got := deny(); len(got) != 0 {
			t.Fatalf("Settings[repo].Deny with no settings file = %v, want none", got)
		}
		pushSettingsFile(t, f.repoPath, "deny = [\"go.mod\"]\n")
		if diff := cmp.Diff([]string{"go.mod"}, deny()); diff != "" {
			t.Errorf("Settings[repo].Deny after main changed mismatch (-want +got):\n%s", diff)
		}
		gittest.Run(t, f.repoPath, "switch", "-q", "-c", "ticket")
		pushSettingsFile(t, f.repoPath, "deny = []\n")
		gittest.Run(t, f.repoPath, "push", "-q", "origin", "ticket")
		if diff := cmp.Diff([]string{"go.mod"}, deny()); diff != "" {
			t.Errorf("Settings[repo].Deny after a branch changed mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("hashes .mergify.yml at origin/main, not the working tree", func(t *testing.T) {
		t.Parallel()
		f := newObserverFixture(t)
		gittest.Commit(t, f.repoPath, ".mergify.yml", "pull_request_rules: []\n")
		pushSettingsFile(t, f.repoPath, "mergify_sha = \"pinned\"\n")
		clean := f.observation(t).MergifyHash["repo"]
		if clean == "" {
			t.Fatal("MergifyHash[repo] = \"\", want a hash")
		}

		mergify := filepath.Join(f.repoPath, ".mergify.yml")
		if err := os.WriteFile(mergify, []byte("pull_request_rules: [tampered]\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", mergify, err)
		}
		if got := f.observation(t).MergifyHash["repo"]; got != clean {
			t.Errorf("MergifyHash[repo] with a dirty working tree = %q, want %q", got, clean)
		}

		gittest.Run(t, f.repoPath, "checkout", "-q", "--", ".mergify.yml")
		gittest.Run(t, f.repoPath, "checkout", "-q", "-b", "elsewhere")
		gittest.Run(t, f.repoPath, "rm", "-q", ".mergify.yml")
		gittest.Run(t, f.repoPath, "commit", "-q", "-m", "drop mergify config on another branch")
		if got := f.observation(t).MergifyHash["repo"]; got != clean {
			t.Errorf("MergifyHash[repo] on another branch = %q, want %q", got, clean)
		}
	})

	t.Run("one repo's fetch failure carries its last observation and leaves the others observed", func(t *testing.T) {
		t.Parallel()
		root, _ := repoWithOrigin(t)
		if err := os.MkdirAll(config.CheckoutPath(root, "acme/broken"), 0o700); err != nil {
			t.Fatalf("MkdirAll(acme/broken): %v", err)
		}
		cfg, _ := testConfigAndWorkspace(t, root, 1, []string{"true"})
		st := openStore(t)
		trackRepo(t, st, "repo", root+"/remote.git")
		upsertRepoAs(t, st, "acme/broken", plan.RepoReady)
		brokenPR := branchRef("acme/broken", "cc-1")
		previous := plan.Observation{PRs: map[plan.BranchRef]plan.PR{brokenPR: {Number: 7, State: plan.Open}}}
		if err := st.SaveObservation(t.Context(), previous); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		obs, err := loop.NewObserver(st, &ghtest.Forge{}, cfg)(t.Context())
		if err != nil {
			t.Fatalf("observe() error = %v", err)
		}
		if obs.PRs[brokenPR].Number != 7 {
			t.Errorf("PRs[%v].Number = %d, want 7 carried forward", brokenPR, obs.PRs[brokenPR].Number)
		}
		if _, ok := obs.Settings["repo"]; !ok {
			t.Errorf("Settings has no repo, want it observed despite acme/broken failing")
		}
		if len(obs.RepoErrors) != 1 || obs.RepoErrors["acme/broken"] == "" {
			t.Errorf("RepoErrors = %+v, want exactly acme/broken", obs.RepoErrors)
		}
	})

	t.Run("reads peer conflicts, reusing the last read while both tips are unmoved", func(t *testing.T) {
		t.Parallel()
		f := newObserverFixture(t)
		storetest.Tickets(t, f.store,
			storepkg.Ticket{URL: "sandbox://A", Repo: "repo", Branch: "a"},
			storepkg.Ticket{URL: "sandbox://B", Repo: "repo", Branch: "b"},
		)
		a, b := branchRef("repo", "a"), branchRef("repo", "b")
		f.pushBranch(t, "a", "shared.txt", "from a\n")
		f.pushBranch(t, "b", "shared.txt", "from b\n")

		first := f.observation(t)
		if !first.ConflictsWithPeer[a][b] || !first.ConflictsWithPeer[b][a] {
			t.Fatalf("ConflictsWithPeer = %v, want a and b conflicting", first.ConflictsWithPeer)
		}

		stale := first
		stale.ConflictsWithPeer = map[plan.BranchRef]map[plan.BranchRef]bool{a: {b: false}, b: {a: false}}
		if err := f.store.SaveObservation(t.Context(), stale); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		if got := f.observation(t).ConflictsWithPeer[a][b]; got {
			t.Errorf("ConflictsWithPeer[a][b] with unmoved tips = %v, want the last read (false) reused", got)
		}

		f.pushBranch(t, "a", "shared.txt", "from a, again\n")
		if got := f.observation(t).ConflictsWithPeer[a][b]; !got {
			t.Errorf("ConflictsWithPeer[a][b] after a moved = %v, want it read afresh (true)", got)
		}
	})
}
