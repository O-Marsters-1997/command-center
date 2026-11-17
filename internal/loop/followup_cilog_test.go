package loop_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

type failingFailedLog struct{ *ghtest.Forge }

func (f failingFailedLog) FailedLog(_ context.Context, repo, detailsURL string) (string, error) {
	f.Record(ghtest.Call{Method: "FailedLog", Repo: repo, Body: detailsURL})
	return "", fmt.Errorf("fake gh: run view failed: %w", errForge)
}

func (f *pushFixture) seedCIFailed(t *testing.T, detailsURL string) {
	t.Helper()
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	storetest.Pushed(t, f.Store, storetest.PushedOpts{
		URL: f.Ticket.URL, Tip: "cc-1-tip", Base: "main", BaseTip: "main-tip", At: testAt,
	})
	checks := map[string]plan.CheckState{
		"CI": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: detailsURL},
	}
	*f.Obs = draftConsumerPR(plan.Merged, checks, "cc-1-tip")
	f.Obs.Worktrees = map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.Worktree}
	if err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "fix the failing test", At: testAt,
	}); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
}

func TestFollowUp_CIFailed(t *testing.T) {
	t.Parallel()

	t.Run("carries the last 200 lines of the failed log", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		var lines []string
		for i := 1; i <= 250; i++ {
			lines = append(lines, fmt.Sprintf("line%d", i))
		}
		f.Forge.Logs = map[string]string{ghtest.FailedRunURL: strings.Join(lines, "\n") + "\n"}
		f.seedCIFailed(t, ghtest.FailedRunURL)
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		prompt := spawns[0].Prompt
		for _, want := range []string{"fix the failing test", "## Failed CI log: CI", "line51\n", "line250"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("prompt = %q, want it to mention %q", prompt, want)
			}
		}
		if strings.Contains(prompt, "line50\n") {
			t.Errorf("prompt = %q, want only the last 200 lines", prompt)
		}
		if got := f.events(t, "ci_log_unavailable"); len(got) != 0 {
			t.Errorf("ci_log_unavailable events = %+v, want none", got)
		}
	})

	t.Run("spawns and says so when the log cannot be fetched", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedCIFailed(t, ghtest.FailedRunURL)
		fake := &runnertest.Runner{}
		f.reset(testAt, fake, loop.WithForge(failingFailedLog{f.Forge}))
		f.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 || !strings.Contains(spawns[0].Prompt, "could not be retrieved") {
			t.Fatalf("spawns = %+v, want one whose prompt says the log was unavailable", spawns)
		}
		events := f.events(t, "ci_log_unavailable")
		if len(events) != 1 || !strings.Contains(events[0].Detail, "CI") ||
			!strings.Contains(events[0].Detail, fmt.Sprint(errForge)) {
			t.Errorf("ci_log_unavailable events = %+v, want one naming the check and the error", events)
		}
	})

	t.Run("spawns and says so when the check has no Actions run id", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedCIFailed(t, "https://example.com/status/1")
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 || !strings.Contains(spawns[0].Prompt, "could not be retrieved") {
			t.Fatalf("spawns = %+v, want one whose prompt says the log was unavailable", spawns)
		}
		if got := f.events(t, "ci_log_unavailable"); len(got) != 1 {
			t.Errorf("ci_log_unavailable events = %+v, want one", got)
		}
	})

	t.Run("a follow-up from any other state fetches no log", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		if err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "tidy up", At: testAt,
		}); err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		f.reset(testAt, &runnertest.Runner{})
		f.tick(t)

		if got := f.Forge.Calls("FailedLog"); len(got) != 0 {
			t.Errorf("FailedLog calls = %+v, want none", got)
		}
	})
}
