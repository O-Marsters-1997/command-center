package loop_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
)

func TestReRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		prepare  func(t *testing.T, f *pushFixture)
		wantPath func(f *pushFixture) string
	}{
		{
			name:     "existing worktree",
			prepare:  func(*testing.T, *pushFixture) {},
			wantPath: func(f *pushFixture) string { return f.Worktree },
		},
		{
			name: "gone worktree is cut afresh",
			prepare: func(t *testing.T, f *pushFixture) {
				t.Helper()
				gittest.Run(t, f.Repo, "worktree", "remove", "--force", f.Worktree)
				delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))
			},
			wantPath: func(f *pushFixture) string { return filepath.Join(filepath.Dir(f.Repo), "wt-"+f.Ticket.Branch) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			worktrees := &tptest.Worktrees{}
			f := newPushFixture(t, loop.WithWorktrees(worktrees))
			firstRunID, err := f.Store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
				Ticket: f.Ticket.URL, Kind: "agent", PromptHash: "hash-1",
			})
			if err != nil {
				t.Fatalf("InsertRunSkeleton: %v", err)
			}
			spawn := storepkg.Spawn{RunID: plan.RunID(firstRunID), Pgid: 111, StartedAt: testAt, LogPath: "/state/runs/1.jsonl"}
			if err := f.Store.RecordSpawn(t.Context(), spawn); err != nil {
				t.Fatalf("RecordSpawn: %v", err)
			}
			if err := f.Store.RecordDisposition(t.Context(), storepkg.Disposition{
				RunID: plan.RunID(firstRunID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(1),
				Cost: option.None[float64](), EndedAt: testAt,
			}); err != nil {
				t.Fatalf("RecordDisposition: %v", err)
			}
			if err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{Ticket: f.Ticket.URL, Verb: "re-run", At: testAt.Add(time.Second)}); err != nil {
				t.Fatalf("QueueVerbIntent: %v", err)
			}
			tt.prepare(t, f)
			fake := &runnertest.Runner{}
			f.reset(testAt.Add(time.Second), fake, loop.WithWorktrees(worktrees))
			f.tick(t)

			spawns := fake.Spawns()
			if len(spawns) != 1 {
				t.Fatalf("spawns = %d, want 1", len(spawns))
			}
			want := tt.wantPath(f)
			want, err = filepath.EvalSymlinks(want)
			if err != nil {
				t.Fatalf("EvalSymlinks(want worktree): %v", err)
			}
			got, err := filepath.EvalSymlinks(spawns[0].WorktreePath)
			if err != nil {
				t.Fatalf("EvalSymlinks(spawn worktree): %v", err)
			}
			if got != want {
				t.Errorf("re-run worktree = %q, want %q", got, want)
			}
			if spawns[0].SystemPromptPath != f.ws.SystemPromptPath {
				t.Errorf("system prompt path = %q, want %q", spawns[0].SystemPromptPath, f.ws.SystemPromptPath)
			}
			latest, err := f.Store.LatestRunsByTicket(t.Context())
			if err != nil {
				t.Fatalf("LatestRunsByTicket: %v", err)
			}
			summary, ok := latest[plan.TicketURL(f.Ticket.URL)]
			if !ok {
				t.Fatal("no run recorded after re-run")
			}
			if int64(summary.ID) == firstRunID {
				t.Error("re-run must create a second runs row, not reuse the first")
			}
			if summary.Outcome.IsSome() {
				t.Error("the freshly spawned re-run must not already have an outcome")
			}
			if events := f.events(t, "re_run_refused"); len(events) != 0 {
				t.Errorf("re-run refusals = %+v, want none", events)
			}
			pending, err := f.Store.PendingVerbIntents(t.Context(), "re-run")
			if err != nil {
				t.Fatalf("PendingVerbIntents: %v", err)
			}
			if len(pending) != 0 {
				t.Errorf("pending re-run intents = %+v, want none: consumed", pending)
			}
		})
	}
}
