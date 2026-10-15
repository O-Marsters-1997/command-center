package loop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func TestFailedVerificationDoesNotPush(t *testing.T) {
	t.Parallel()
	type scenario struct {
		ticketURL, branch, originalTip, detail string
		command                                []string
		mergedFile, needle                     string
		wantCount                              int
	}
	tests := []struct {
		name  string
		setup func(*testing.T, *fixture) scenario
	}{
		{
			name: "failed restack command",
			setup: func(t *testing.T, p *fixture) scenario {
				t.Helper()
				f := newStackedFixture(t, p.Repo, p.Store, testAt)
				childTip := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
				parentTip := advanceParent(t, p.Repo, f)
				*p.Obs = baseObservation(f, parentTip)
				return scenario{ticketURL: f.child.URL, branch: "child", originalTip: childTip,
					command: []string{"sh", "-c", "echo 'undefined: dup' >&2; exit 1"}, detail: "undefined: dup",
					mergedFile: filepath.Join(f.childWorktree, "child.txt"), needle: "child's own work", wantCount: 1}
			},
		},
		{
			name: "clean merge with duplicate declaration fails go vet",
			setup: func(t *testing.T, p *fixture) scenario {
				t.Helper()
				root, repoPath := p.Root, p.Repo
				gittest.Commit(t, repoPath, "go.mod", "module fixture\n\ngo 1.21\n")
				gittest.Commit(t, repoPath, "helpers.go", "package fixture\n\nfunc Base() {}\n")
				gittest.Run(t, repoPath, "push", "-q", "origin", "main")
				f := newStackedFixture(t, repoPath, p.Store, testAt)
				gittest.Commit(t, f.parentWorktree, "helpers.go", "package fixture\n\nfunc Base() {}\n\nfunc Dup() {}\n")
				gittest.Run(t, repoPath, "push", "-q", "origin", "parent")
				advanceMain(t, root, "helpers.go", "package fixture\n\nfunc Dup() {}\n\nfunc Base() {}\n")
				gittest.Run(t, repoPath, "fetch", "-q", "origin", "main")
				mainTip := gittest.Run(t, repoPath, "rev-parse", "refs/remotes/origin/main")
				*p.Obs = baseObservation(f, f.parentTip0)
				p.Obs.BranchTips[branchRef("repo", "main")] = plan.SHA(mainTip)
				goBinary, err := exec.LookPath("go")
				if err != nil {
					t.Fatalf("LookPath go: %v", err)
				}
				return scenario{ticketURL: f.parent.URL, branch: "parent",
					originalTip: gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/parent"),
					command:     []string{goBinary, "vet", "./..."}, detail: "Dup redeclared",
					mergedFile: filepath.Join(f.parentWorktree, "helpers.go"), needle: "func Dup()", wantCount: 2}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := newPushFixture(t)
			s := tt.setup(t, p)
			cfg, ws := testConfigAndWorkspace(t, p.Root, 0, nil)
			p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: s.command}
			p.Cfg, p.WS = cfg, ws
			p.reset(testAt.Add(time.Minute), runner.ProcessRunner{})
			p.Tick(t)
			merged, err := os.ReadFile(s.mergedFile)
			if err != nil {
				t.Fatalf("ReadFile %s: %v", s.mergedFile, err)
			}
			if got := strings.Count(string(merged), s.needle); got != s.wantCount {
				t.Errorf("merged file %s contains %q %d times, want %d", s.mergedFile, s.needle, got, s.wantCount)
			}
			events := p.events(t, "verification_failed")
			if len(events) != 1 || events[0].TicketURL != s.ticketURL ||
				!strings.Contains(events[0].Detail, s.detail) ||
				!strings.Contains(events[0].Detail, strings.Join(s.command[:2], " ")) {
				t.Errorf("events = %+v, want verification_failed for %s naming %s", events, s.ticketURL, s.detail)
			}
			for _, event := range p.events(t, "pushed") {
				if event.TicketURL == s.ticketURL {
					t.Errorf("pushed event = %+v, want none for %s", event, s.ticketURL)
				}
			}
			remoteTip := gittest.Run(t, filepath.Join(p.Root, "remote.git"), "rev-parse", "refs/heads/"+s.branch)
			if remoteTip != s.originalTip {
				t.Errorf("remote %s tip = %s, want unchanged %s", s.branch, remoteTip, s.originalTip)
			}
			page := renderPage(t, openServer(p.Store, cctest.Clock{At: testAt.Add(time.Minute)}, ""))
			row := webtest.RowFor(t, page, s.ticketURL)
			if row.State != "verification_failed" {
				t.Errorf("row state = %q, want verification_failed:\n%s", row.State, row.Row.HTML())
			}
			for _, verb := range []plan.Verb{plan.VerbRetryPush, plan.VerbReRun} {
				if !strings.Contains(page, `value="`+string(verb)+`"`) {
					t.Errorf("page does not offer %s:\n%s", verb, page)
				}
			}
		})
	}
}

func TestFailedVerificationTruncatesAtValidUTF8(t *testing.T) {
	t.Parallel()
	for _, spacing := range []string{" ", "  "} {
		t.Run("exit spacing "+spacing, func(t *testing.T) {
			t.Parallel()
			p := newPushFixture(t)
			f := newStackedFixture(t, p.Repo, p.Store, testAt)
			parentTip := advanceParent(t, p.Repo, f)
			*p.Obs = baseObservation(f, parentTip)
			command := []string{"sh", "-c", "awk 'BEGIN { for (i=0; i<3000; i++) printf \"é\" }';" + spacing + "exit 1"}
			cfg, ws := testConfigAndWorkspace(t, p.Root, 0, nil)
			p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: command}
			p.Cfg, p.WS = cfg, ws
			p.reset(testAt.Add(time.Minute), runner.ProcessRunner{})
			p.Tick(t)
			events := p.events(t, "verification_failed")
			if len(events) != 1 {
				t.Fatalf("verification_failed events = %d, want 1", len(events))
			}
			if !utf8.ValidString(events[0].Detail) {
				t.Error("utf8.ValidString(verification detail) = false, want true")
			}
		})
	}
}

func TestARepoWithNoVerifyCommandConfiguredIsUnaffected(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.Root, p.Repo, p.Store
	at := testAt

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.Cfg, p.WS = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.Tick(t)

	if events := p.events(t, "verification_failed"); len(events) != 0 {
		t.Errorf("events = %+v, want no verification_failed event: no command was configured", events)
	}
	childTip := p.head(t, f.childWorktree, "HEAD")
	events := p.events(t, "pushed")
	childPushed := false
	for _, event := range events {
		if event.TicketURL == f.child.URL && event.Detail == "pushed "+childTip+" to origin/parent" {
			childPushed = true
		}
	}
	if !childPushed {
		t.Errorf("events = %+v, want the merge pushed as normal", events)
	}
}

func TestVerificationRunsOnTheRestackNotOnAnAlreadyVerifiedTip(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.Root, p.Repo, p.Store
	at := testAt

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	countFile := filepath.Join(t.TempDir(), "count")
	countScript := "n=$(cat \"" + countFile + "\" 2>/dev/null || echo 0); " +
		"echo $((n+1)) > \"" + countFile + "\"; exit 1"
	command := []string{"sh", "-c", countScript}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: command}
	p.Cfg, p.WS = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.Tick(t)
	p.Tick(t)

	got, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("ReadFile count: %v", err)
	}
	if want := "1\n"; string(got) != want {
		t.Errorf("verify command ran %q times, want %q: the automatic pass must not reverify "+
			"an already-failed tip every tick", got, want)
	}
}

func TestRetryPushAfterAFailedVerificationClearsTheLatch(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.Root, p.Repo, p.Store
	at := testAt

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: []string{"false"}}
	p.Cfg, p.WS = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.Tick(t)

	facts, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatalf("RefreshFacts: %v", err)
	}
	if !facts[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Fatalf("refresh facts = %+v, want %s verification failed", facts, f.child.URL)
	}

	if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: f.child.URL, Verb: plan.VerbRetryPush, At: at.Add(time.Hour),
	}); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	p.Tick(t)

	events := p.events(t, "pushed")
	childTip := p.head(t, f.childWorktree, "HEAD")
	pushedChild := false
	for _, e := range events {
		if e.TicketURL == f.child.URL && e.Detail == "pushed "+childTip+" to origin/parent" {
			pushedChild = true
		}
	}
	if !pushedChild {
		t.Errorf("events = %+v, want retry-push to push %s despite the failed verification", events, f.child.URL)
	}

	factsAfter, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatalf("RefreshFacts: %v", err)
	}
	if factsAfter[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Errorf("refresh facts = %+v, want the latch cleared by the push", factsAfter)
	}
}
