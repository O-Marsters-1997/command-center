package cc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// buildCommandRegenerating is a fake build_command: it overwrites relPath with a fixed,
// deterministic result, standing in for `just assets` so the test never depends on this repo's
// own toolchain.
func buildCommandRegenerating(relPath, content string) []string {
	return []string{"sh", "-c", "printf '%s' " + shQuote(content) + " > " + shQuote(relPath)}
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const regeneratedContent = "regenerated content\n"

// generatedConflictFixture cuts a ticket branch off origin/main that edits a "generated" file
// (and, when extraHandwritten is set, a hand-written one too), pushes and records it, then
// advances main with a conflicting edit to the same paths -- the shape a repo's build output
// conflicts in unconditionally, whatever the underlying rules do.
type generatedConflictFixture struct {
	ticket       cc.Ticket
	worktreePath string
	branchTip0   string
	root         string
	repoPath     string
}

func newGeneratedConflictFixture(
	t *testing.T, store *cc.Store, at time.Time, extraHandwritten, recordPush bool,
) generatedConflictFixture {
	t.Helper()
	ctx := context.Background()

	root, repoPath := repoWithOrigin(t)
	commitFile(t, repoPath, "dist/app.css", "base\n")
	if extraHandwritten {
		commitFile(t, repoPath, "handwritten.go", "package x\n\nconst n = 1\n")
	}
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	mainSHA0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "dist/app.css", "the branch's own regenerated output\n")
	if extraHandwritten {
		commitFile(t, worktreePath, "handwritten.go", "package x\n\nconst n = 2\n")
	}
	runGit(t, "-C", repoPath, "push", "-q", "origin", "cc-1")
	branchTip0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	dispositionAsPushed(t, store, ticket.URL, at)
	if recordPush {
		if err := store.RecordPush(ctx, ticket.URL, branchTip0, "main", mainSHA0, at); err != nil {
			t.Fatal(err)
		}
	}

	advanceMain(t, root, "dist/app.css", "someone else's regenerated output\n")
	if extraHandwritten {
		advanceMainFile(t, root, "handwritten.go", "package x\n\nconst n = 3\n")
	}
	runGit(t, "-C", repoPath, "fetch", "-q", "origin", "main")

	return generatedConflictFixture{
		ticket: ticket, worktreePath: worktreePath, branchTip0: branchTip0, root: root, repoPath: repoPath,
	}
}

// advanceMainFile pushes a second commit onto main from the same throwaway clone advanceMain
// uses, so two conflicting paths can be advanced without main forking into two histories.
func advanceMainFile(t *testing.T, root, relPath, contents string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "main-clone-2")
	runGit(t, "clone", "-q", filepath.Join(root, "remote.git"), clone)
	commitFile(t, clone, relPath, contents)
	runGit(t, "-C", clone, "push", "-q", "origin", "HEAD:main")
}

// observe recomputes this branch's own conflict-with-main facts fresh via real git on every
// call, the same way NewObserver does, so a tick that resolves the conflict is read as clean by
// the next tick's own observation rather than by a frozen struct.
func (f generatedConflictFixture) observe(t *testing.T) cc.ObserveFunc {
	t.Helper()
	return func(ctx context.Context) (cc.Observation, error) {
		mainTip, err := cc.RevParse(ctx, f.repoPath, "refs/remotes/origin/main")
		if err != nil {
			return cc.Observation{}, err
		}
		branchTip, err := cc.RevParse(ctx, f.repoPath, "refs/heads/cc-1")
		if err != nil {
			return cc.Observation{}, err
		}
		clean, paths, err := cc.MergesCleanly(ctx, f.repoPath, mainTip, branchTip)
		if err != nil {
			return cc.Observation{}, err
		}
		mid, err := cc.MidMerge(ctx, f.worktreePath)
		if err != nil {
			return cc.Observation{}, err
		}
		return cc.Observation{
			Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
			PRs: map[string]gh.PR{
				cc.BranchKey("repo", "cc-1"): {
					Number: 1, HeadRef: "cc-1", State: gh.Open, HeadOid: branchTip,
					Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
				},
			},
			BranchTips: map[string]string{
				cc.MainTipKey("repo"): mainTip, cc.BranchKey("repo", "cc-1"): branchTip,
			},
			Runs:              map[string]cc.RunObservation{},
			MidMerge:          map[string]bool{cc.BranchKey("repo", "cc-1"): mid},
			ConflictsWithBase: map[string]bool{cc.BranchKey("repo", "cc-1"): !clean},
			ConflictedPaths:   map[string][]string{cc.BranchKey("repo", "cc-1"): paths},
		}, nil
	}
}

func (f generatedConflictFixture) configAndWorkspace(
	t *testing.T, generated, buildCommand []string,
) (cc.Config, cc.Workspace) {
	t.Helper()
	cfg := cc.Config{
		Repos: []cc.Repo{{
			Name: "repo", Checkout: f.repoPath,
			Checks:       verdict.Predicate{Success: "CI"},
			Generated:    generated,
			BuildCommand: buildCommand,
		}},
	}
	ws := cc.Workspace{RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	return cfg, ws
}

func TestAConflictConfinedToGeneratedPathsMergesRebuildsCommitsAndPushes(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	f := newGeneratedConflictFixture(t, store, at, false, true)

	cfg, ws := f.configAndWorkspace(t, []string{"dist/**"}, buildCommandRegenerating("dist/app.css", regeneratedContent))
	loop := cc.NewLoop(store, f.observe(t), fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(f.worktreePath, "dist", "app.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != regeneratedContent {
		t.Errorf("dist/app.css = %q, want the build command's own output", got)
	}

	head := strings.TrimSpace(runGitOutput(t, "-C", f.worktreePath, "rev-parse", "HEAD"))
	parents := strings.Fields(runGitOutput(t, "-C", f.worktreePath, "log", "-1", "--format=%P", head))
	if len(parents) != 2 {
		t.Fatalf("HEAD has %d parents, want 2 (a merge commit): %v", len(parents), parents)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want a generated_conflict_resolved event", events)
	}
	if !hasEvent(events, "pushed", head) {
		t.Errorf("events = %+v, want a pushed event naming the merge commit %s: no push code of its own, "+
			"pushPushable delivers it", events, head)
	}

	remoteTip := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(f.root, "remote.git"), "rev-parse", "refs/heads/cc-1"))
	if remoteTip != head {
		t.Errorf("remote cc-1 tip = %s, want %s: the regenerated commit must be pushed the same tick", remoteTip, head)
	}

	// The tick that resolved the conflict still reads its own stale observation; the next tick's
	// fresh read is the one that sees the branch now merges cleanly.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	page := renderPage(t, cc.NewServer(store, fixedClock(at.Add(time.Minute)), cfg.Repos, ""))
	if state := rowState(t, page, f.ticket.URL); state != "review_me" {
		t.Errorf("row state = %q, want review_me: no operator action, no agent spawned", state)
	}
}

func TestAConflictTouchingAHandWrittenPathIsLeftUntouched(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	f := newGeneratedConflictFixture(t, store, at, true, true)

	cfg, ws := f.configAndWorkspace(t, []string{"dist/**"}, buildCommandRegenerating("dist/app.css", regeneratedContent))
	loop := cc.NewLoop(store, f.observe(t), fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.worktreePath, "rev-parse", "HEAD")); got != f.branchTip0 {
		t.Errorf("worktree HEAD = %s, want unchanged %s: a conflicting merge never commits", got, f.branchTip0)
	}

	// The row keeps the state it has today: the pre-existing base-moved refresh still runs and
	// still conflicts for a human, since this ticket's own gate never touched the worktree.
	mid, err := cc.MidMerge(t.Context(), f.worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if !mid {
		t.Error("worktree is not mid-merge: the ordinary refresh conflict this ticket does not own should still have run")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want no generated_conflict_resolved event", events)
	}
}

// TestAGenuineMergeFailureIsReportedRatherThanBuiltOver covers the gap between a conflict
// (git merge-tree's own exit 1, resolved by regenerating) and any other git failure: an
// uncommitted local change to the same path makes the real `git merge` refuse for a different
// reason (exit 2, "local changes would be overwritten"), which must surface as a tick error
// rather than run the build command and commit over a merge that never happened.
func TestAGenuineMergeFailureIsReportedRatherThanBuiltOver(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	f := newGeneratedConflictFixture(t, store, at, false, true)

	if err := os.WriteFile(filepath.Join(f.worktreePath, "dist", "app.css"), []byte("uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, ws := f.configAndWorkspace(t, []string{"dist/**"}, buildCommandRegenerating("dist/app.css", regeneratedContent))
	loop := cc.NewLoop(store, f.observe(t), fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err == nil {
		t.Fatal("RunOnce: want an error, the local change makes the real merge fail for a reason other than a conflict")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want no generated_conflict_resolved event: nothing was actually merged", events)
	}
}

// TestAConflictOnABranchNeverPushedIsLeftAlone covers a tracked branch that conflicts with main
// before pushPushable has anything recorded for it: merging, rebuilding and committing would sit
// unpushed with nothing to surface it, so this gate waits for a first push the way pushPushable
// itself does.
func TestAConflictOnABranchNeverPushedIsLeftAlone(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	f := newGeneratedConflictFixture(t, store, at, false, false)

	cfg, ws := f.configAndWorkspace(t, []string{"dist/**"}, buildCommandRegenerating("dist/app.css", regeneratedContent))
	loop := cc.NewLoop(store, f.observe(t), fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.worktreePath, "rev-parse", "HEAD")); got != f.branchTip0 {
		t.Errorf("worktree HEAD = %s, want unchanged %s: nothing is pushed for this ticket yet", got, f.branchTip0)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want no generated_conflict_resolved event", events)
	}
}

// TestAStaleWorktreeIsFastForwardedBeforeMergingAGeneratedConflict covers issue #187: a worktree
// behind origin/cc-1 must be caught up before the real merge runs, so it conflicts on the same
// path observe named rather than on whatever the stale HEAD happens to disagree with main about.
func TestAStaleWorktreeIsFastForwardedBeforeMergingAGeneratedConflict(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	root, repoPath := repoWithOrigin(t)
	commitFile(t, repoPath, "handwritten.go", "package x\n\nconst n = 1\n")
	commitFile(t, repoPath, "dist/app.css", "base\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	mainSHA0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "handwritten.go", "package x\n\nconst n = 2\n")
	staleTip := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))

	commitFile(t, worktreePath, "handwritten.go", "package x\n\nconst n = 1\n")
	commitFile(t, worktreePath, "dist/app.css", "the branch's own regenerated output\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "cc-1")
	pushedTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	runGit(t, "-C", worktreePath, "reset", "--hard", staleTip)

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	dispositionAsPushed(t, store, ticket.URL, at)
	if err := store.RecordPush(ctx, ticket.URL, pushedTip, "main", mainSHA0, at); err != nil {
		t.Fatal(err)
	}

	advanceMain(t, root, "dist/app.css", "main's own regenerated output\n")
	advanceMainFile(t, root, "handwritten.go", "package x\n\nconst n = 3\n")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin", "main")

	observe := func(ctx context.Context) (cc.Observation, error) {
		mainTip, err := cc.RevParse(ctx, repoPath, "refs/remotes/origin/main")
		if err != nil {
			return cc.Observation{}, err
		}
		branchTip, err := cc.RevParse(ctx, repoPath, "refs/remotes/origin/cc-1")
		if err != nil {
			return cc.Observation{}, err
		}
		clean, paths, err := cc.MergesCleanly(ctx, repoPath, mainTip, branchTip)
		if err != nil {
			return cc.Observation{}, err
		}
		mid, err := cc.MidMerge(ctx, worktreePath)
		if err != nil {
			return cc.Observation{}, err
		}
		return cc.Observation{
			Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath},
			PRs: map[string]gh.PR{
				cc.BranchKey("repo", "cc-1"): {
					Number: 1, HeadRef: "cc-1", State: gh.Open, HeadOid: branchTip,
					Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
				},
			},
			BranchTips: map[string]string{
				cc.MainTipKey("repo"): mainTip, cc.BranchKey("repo", "cc-1"): branchTip,
			},
			Runs:              map[string]cc.RunObservation{},
			MidMerge:          map[string]bool{cc.BranchKey("repo", "cc-1"): mid},
			ConflictsWithBase: map[string]bool{cc.BranchKey("repo", "cc-1"): !clean},
			ConflictedPaths:   map[string][]string{cc.BranchKey("repo", "cc-1"): paths},
		}, nil
	}

	cfg := cc.Config{
		Repos: []cc.Repo{{
			Name: "repo", Checkout: repoPath,
			Checks:       verdict.Predicate{Success: "CI"},
			Generated:    []string{"dist/**"},
			BuildCommand: buildCommandRegenerating("dist/app.css", regeneratedContent),
		}},
	}
	ws := cc.Workspace{RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(worktreePath, "dist", "app.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != regeneratedContent {
		t.Errorf("dist/app.css = %q, want the build command's own output", got)
	}
	gotHandwritten, err := os.ReadFile(filepath.Join(worktreePath, "handwritten.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotHandwritten) != "package x\n\nconst n = 3\n" {
		t.Errorf("handwritten.go = %q, want main's own version merged in cleanly", gotHandwritten)
	}

	mid, err := cc.MidMerge(t.Context(), worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if mid {
		t.Error("worktree is mid-merge: fast-forwarding first should let the merge complete cleanly")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want a generated_conflict_resolved event", events)
	}

	remoteTip := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/cc-1"))
	head := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))
	if remoteTip != head {
		t.Errorf("remote cc-1 tip = %s, want %s: the regenerated commit must be pushed the same tick", remoteTip, head)
	}
}

// TestAMergeThatUnexpectedlyConflictsOutsideTheGeneratedSetAborts covers issue #187's second
// requirement: a merge whose real, post-run unmerged set reaches outside the generated paths
// must be aborted rather than left for MidMerge to find and skip forever.
func TestAMergeThatUnexpectedlyConflictsOutsideTheGeneratedSetAborts(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	store := openStore(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	root, repoPath := repoWithOrigin(t)
	commitFile(t, repoPath, "handwritten.go", "package x\n\nconst n = 1\n")
	commitFile(t, repoPath, "dist/app.css", "base\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	mainSHA0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "handwritten.go", "package x\n\nconst n = 2\n")
	commitFile(t, worktreePath, "dist/app.css", "the branch's own regenerated output\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "cc-1")
	branchTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, ticket.URL, branchTip, "main", mainSHA0, at); err != nil {
		t.Fatal(err)
	}

	advanceMain(t, root, "dist/app.css", "main's own regenerated output\n")
	advanceMainFile(t, root, "handwritten.go", "package x\n\nconst n = 3\n")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin", "main")

	// Deliberately wrong: the real merge below conflicts on handwritten.go too.
	observe := func(ctx context.Context) (cc.Observation, error) {
		return cc.Observation{
			Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath},
			PRs: map[string]gh.PR{
				cc.BranchKey("repo", "cc-1"): {
					Number: 1, HeadRef: "cc-1", State: gh.Open, HeadOid: branchTip,
					Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
				},
			},
			Runs:              map[string]cc.RunObservation{},
			MidMerge:          map[string]bool{cc.BranchKey("repo", "cc-1"): false},
			ConflictsWithBase: map[string]bool{cc.BranchKey("repo", "cc-1"): true},
			ConflictedPaths:   map[string][]string{cc.BranchKey("repo", "cc-1"): {"dist/app.css"}},
		}, nil
	}

	cfg := cc.Config{
		Repos: []cc.Repo{{
			Name: "repo", Checkout: repoPath,
			Checks:       verdict.Predicate{Success: "CI"},
			Generated:    []string{"dist/**"},
			BuildCommand: buildCommandRegenerating("dist/app.css", regeneratedContent),
		}},
	}
	ws := cc.Workspace{RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	mid, err := cc.MidMerge(t.Context(), worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if mid {
		t.Error("worktree is mid-merge: a merge landing outside the generated set must be aborted, not left")
	}

	head := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))
	if head != branchTip {
		t.Errorf("worktree HEAD = %s, want unchanged %s: the aborted merge must not leave a commit", head, branchTip)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "generated_conflict_resolved", "") {
		t.Errorf("events = %+v, want no generated_conflict_resolved event: nothing was actually merged", events)
	}
}
