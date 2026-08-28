package cc

import "testing"

func TestDeriveFleetCardSegmentsSumToTheRowCount(t *testing.T) {
	t.Parallel()

	rows := []row{
		{State: "blocked", Tone: "wait", Unattended: false},
		{State: "ready", Tone: "idle", Unattended: false},
		{State: "queued", Tone: "wait", Unattended: true},
		{State: "queued", Tone: "wait", Unattended: true},
	}
	got := deriveFleetCard(rows)

	if got.Total != len(rows) {
		t.Fatalf("Total = %d, want %d", got.Total, len(rows))
	}
	sum := 0
	for _, seg := range got.Segments {
		sum += seg.Count
	}
	if sum != got.Total {
		t.Errorf("segment counts sum to %d, want %d (the row count)", sum, got.Total)
	}
	if got.Yours != 2 {
		t.Errorf("Yours = %d, want 2 (the two rows that are not unattended)", got.Yours)
	}
}

func TestDeriveFleetCardIsEmptyWithNoRows(t *testing.T) {
	t.Parallel()

	got := deriveFleetCard(nil)
	if got.Total != 0 || len(got.Segments) != 0 {
		t.Errorf("deriveFleetCard(nil) = %+v, want a zero-value card", got)
	}
}

func TestDeriveStackCardReadsNoWorktreeCutAsEmpty(t *testing.T) {
	t.Parallel()

	rows := []row{{StackDepth: 2}, {StackDepth: 0}}
	got := deriveStackCard(rows)

	if got.HasWorktree {
		t.Errorf("HasWorktree = true, want false: no row has a cut worktree")
	}
	if got.Deepest != 0 || got.Bars != nil {
		t.Errorf("deriveStackCard with no worktree cut = %+v, want the zero-value card", got)
	}
}

func TestDeriveStackCardHistogramsByDepth(t *testing.T) {
	t.Parallel()

	rows := []row{
		{StackDepth: 0, Worktree: "/repos/a"},
		{StackDepth: 0, Worktree: "/repos/b"},
		{StackDepth: 1, Worktree: "/repos/c"},
		{StackDepth: 3, Worktree: ""},
	}
	got := deriveStackCard(rows)

	if !got.HasWorktree {
		t.Fatalf("HasWorktree = false, want true: CC-1 has a cut worktree")
	}
	if got.Deepest != 3 {
		t.Errorf("Deepest = %d, want 3", got.Deepest)
	}
	want := []depthBar{{Depth: 0, Count: 2, Pct: 100}, {Depth: 1, Count: 1, Pct: 50}, {Depth: 3, Count: 1, Pct: 50}}
	if len(got.Bars) != len(want) {
		t.Fatalf("Bars = %+v, want %+v", got.Bars, want)
	}
	for i, bar := range got.Bars {
		if bar != want[i] {
			t.Errorf("Bars[%d] = %+v, want %+v", i, bar, want[i])
		}
	}
}

func TestDeriveChecksCardCountsGreenAndRedOffCompletedChecks(t *testing.T) {
	t.Parallel()

	rows := []row{
		{Checks: []check{
			{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "lint", Status: "COMPLETED", Conclusion: "FAILURE"},
		}},
		{Checks: []check{{Name: "ci", Status: "COMPLETED", Conclusion: "SKIPPED"}}},
		{Checks: []check{{Name: "ci", Status: "IN_PROGRESS"}}},
		{},
	}
	got := deriveChecksCard(rows)

	if got.Total != len(rows) {
		t.Errorf("Total = %d, want %d", got.Total, len(rows))
	}
	if got.Reported != 3 {
		t.Errorf("Reported = %d, want 3: the rows carrying at least one check", got.Reported)
	}
	if got.Green != 2 {
		t.Errorf("Green = %d, want 2 (SUCCESS + SKIPPED)", got.Green)
	}
	if got.Red != 1 {
		t.Errorf("Red = %d, want 1 (FAILURE)", got.Red)
	}
}

func TestDeriveChecksCardIsEmptyWhenNoBranchHasReported(t *testing.T) {
	t.Parallel()

	got := deriveChecksCard([]row{{}, {}})
	if got.Reported != 0 || got.Green != 0 || got.Red != 0 {
		t.Errorf("deriveChecksCard with no checks = %+v, want an unreported card", got)
	}
}
