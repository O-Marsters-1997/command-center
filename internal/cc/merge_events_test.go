package cc_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestRecordMergedEventsAppendsOnceWithGitHubsMergeTime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	tickAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	observed := plan.Observation{
		PRs: map[string]plan.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Merged, MergedAt: mergedAt},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(tickAt), cc.Config{}, cc.Workspace{}, runner.ProcessRunner{})

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	merges := mergedEvents(events)
	if len(merges) != 1 {
		t.Fatalf("pr_merged events = %d, want exactly 1: %+v", len(merges), events)
	}
	if merges[0].TicketURL != ticket.URL {
		t.Errorf("ticket url = %q, want %q", merges[0].TicketURL, ticket.URL)
	}
	if !merges[0].At.Equal(mergedAt) {
		t.Errorf("event time = %s, want GitHub's own merge time %s, not the tick clock %s",
			merges[0].At, mergedAt, tickAt)
	}

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	events, err = store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 1 {
		t.Errorf("pr_merged events after a second tick = %d, want still 1", got)
	}
}

func TestRecordMergedEventsDedupeSurvivesAClearedMetaTable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	mergedAt := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	observed := plan.Observation{
		PRs: map[string]plan.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Merged, MergedAt: mergedAt},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(mergedAt.Add(time.Hour)), cc.Config{}, cc.Workspace{}, runner.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`DELETE FROM meta`); err != nil {
		t.Fatalf("simulate a restart onto a reset meta table: %v", err)
	}

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce after clearing meta: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 1 {
		t.Errorf("pr_merged events after a cleared meta table = %d, want still 1: %+v", got, events)
	}
}

func TestRecordMergedEventsSkipsAnUnmergedPR(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	observed := plan.Observation{
		PRs: map[string]plan.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Closed},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Now()), cc.Config{}, cc.Workspace{}, runner.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 0 {
		t.Errorf("pr_merged events for a closed-unmerged PR = %d, want 0: %+v", got, events)
	}
}

func TestRecordMergedEventsRecordsHandChurnFromCommitsAfterCCsLastPush(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	dir := initRepoForHandChurnTest(t)
	ccTip := commitFileForHandChurnTest(t, dir, "a.txt", "cc content\n", "cc commit")

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	if err := store.RecordPush(ctx, ticket.URL, ccTip, "main", ccTip, time.Now()); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}

	handTip := commitFileForHandChurnTest(t, dir, "b.txt", "line one\nline two\n", "human commit")

	observed := plan.Observation{
		PRs: map[string]plan.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Merged, MergedAt: time.Now(), HeadOid: handTip},
		},
	}
	cfg := cc.Config{Repos: []cc.Repo{{Name: "cc-sandbox", Checkout: dir}}}
	loop := cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Now()), cfg, cc.Workspace{}, runner.ProcessRunner{})

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := ticketByURL(t, store, ticket.URL)
	if got.HandChurnLines == nil || *got.HandChurnLines != 2 {
		t.Fatalf("hand_churn_lines = %v, want 2", got.HandChurnLines)
	}
}

func TestRecordMergedEventsRecordsZeroHandChurnWhenNothingLandsAfterCCsLastPush(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	dir := initRepoForHandChurnTest(t)
	ccTip := commitFileForHandChurnTest(t, dir, "a.txt", "cc content\n", "cc commit")

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	if err := store.RecordPush(ctx, ticket.URL, ccTip, "main", ccTip, time.Now()); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}

	observed := plan.Observation{
		PRs: map[string]plan.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Merged, MergedAt: time.Now(), HeadOid: ccTip},
		},
	}
	cfg := cc.Config{Repos: []cc.Repo{{Name: "cc-sandbox", Checkout: dir}}}
	loop := cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Now()), cfg, cc.Workspace{}, runner.ProcessRunner{})

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := ticketByURL(t, store, ticket.URL)
	if got.HandChurnLines == nil || *got.HandChurnLines != 0 {
		t.Fatalf("hand_churn_lines = %v, want 0", got.HandChurnLines)
	}
}

func initRepoForHandChurnTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runHandChurnGit(t, dir, "init", "-q", "-b", "main")
	runHandChurnGit(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	return dir
}

func commitFileForHandChurnTest(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runHandChurnGit(t, dir, "add", "-A")
	runHandChurnGit(t, dir, "commit", "-q", "-m", msg)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return trimNewlineForHandChurnTest(string(out))
}

func runHandChurnGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func trimNewlineForHandChurnTest(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func mergedEvents(events []cc.Event) []cc.Event {
	var merges []cc.Event
	for _, e := range events {
		if e.Kind == "pr_merged" {
			merges = append(merges, e)
		}
	}
	return merges
}
