package insight_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
)

func TestParkReason(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, detail, want string }{
		{"verify log path", "verify_command failed; log at /data/logs/12.log", "verify_command failed"},
		{"branch name", "issue-12/foo no longer merges cleanly into main", "… no longer merges cleanly into main"},
		{"workflow hash", "workflow ab12cd is not in this build", "workflow … is not in this build"},
		{"plain text", "waiting on review", "waiting on review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := insight.ParkReason(tt.detail); got != tt.want {
				t.Errorf("ParkReason(%q) = %q, want %q", tt.detail, got, tt.want)
			}
		})
	}
}

func TestParkLedger(t *testing.T) {
	t.Parallel()
	parks := []insight.ParkFact{
		{Reason: "conflict", Detail: "a conflicts", Tokens: 10},
		{Reason: "verify", Detail: "verify 1", Tokens: 500},
		{Reason: "conflict", Detail: "b conflicts", Tokens: 5},
		{Reason: "alpha", Detail: "alpha 1", Tokens: 500},
	}
	want := []insight.ParkRow{
		{Reason: "conflict", Example: "a conflicts", Times: 2, Tokens: 15},
		{Reason: "alpha", Example: "alpha 1", Times: 1, Tokens: 500},
		{Reason: "verify", Example: "verify 1", Times: 1, Tokens: 500},
	}
	if diff := cmp.Diff(want, insight.ParkLedger(parks)); diff != "" {
		t.Errorf("ParkLedger() mismatch (-want +got):\n%s", diff)
	}
	if got := insight.ParkLedger(nil); len(got) != 0 {
		t.Errorf("ParkLedger(nil) = %v, want none", got)
	}
}

func TestJobLedger(t *testing.T) {
	t.Parallel()
	failed := func(ticket, repo, name string) insight.FailedCheck {
		return insight.FailedCheck{Ticket: ticket, Repo: repo, Name: name}
	}
	tests := []struct {
		name    string
		facts   insight.ReworkFacts
		rollups map[string][]string
		want    []insight.JobRow
	}{
		{
			name: "groups and ranks by failures then tokens",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{
					failed("t1", "r", "unit"), failed("t2", "r", "unit"), failed("t1", "r", "lint"),
					failed("t3", "other", "unit"),
				},
				Fixes: []insight.FixRun{{Ticket: "t1", Tokens: 100, Checks: []string{"lint"}}},
			},
			want: []insight.JobRow{
				{Name: "unit", Repo: "r", Failures: 2},
				{Name: "lint", Repo: "r", Failures: 1, FixTokens: 100},
				{Name: "unit", Repo: "other", Failures: 1},
			},
		},
		{
			name: "default rollup glob drops aggregate checks",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{failed("t1", "r", "ci-status-check"), failed("t1", "r", "unit")},
			},
			rollups: map[string][]string{"r": plan.DefaultRollupChecks},
			want:    []insight.JobRow{{Name: "unit", Repo: "r", Failures: 1}},
		},
		{
			name: "repo rollups replace the default",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{
					failed("t1", "r", "ci-status-check"), failed("t1", "r", "gate"), failed("t1", "r", "unit"),
				},
			},
			rollups: map[string][]string{"r": {"g*"}},
			want: []insight.JobRow{
				{Name: "ci-status-check", Repo: "r", Failures: 1},
				{Name: "unit", Repo: "r", Failures: 1},
			},
		},
		{
			name:    "fix tokens split evenly over non-rollup checks and sum to the run",
			rollups: map[string][]string{"r": plan.DefaultRollupChecks},
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{failed("t1", "r", "a")},
				Fixes: []insight.FixRun{
					{Ticket: "t1", Tokens: 100, Checks: []string{"c", "a", "b", "x-status-check"}},
					{Ticket: "t1", Tokens: 7, Checks: []string{"x-status-check"}},
				},
			},
			want: []insight.JobRow{
				{Name: "a", Repo: "r", Failures: 1, FixTokens: 34},
				{Name: "b", Repo: "r", FixTokens: 33},
				{Name: "c", Repo: "r", FixTokens: 33},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := insight.JobLedger(tt.facts, tt.rollups)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("JobLedger() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func pushed(
	merged time.Time, tokens int64, wall time.Duration, refreshes, conflicted int, verdicts ...insight.PushVerdict,
) insight.TicketFact {
	f := fact(merged, tokens, 1, wall, 1)
	for i, v := range verdicts {
		at := merged.Add(time.Duration(i-len(verdicts)) * time.Minute)
		f.Pushes = append(f.Pushes, insight.PushFact{At: at, Verdict: v})
	}
	f.Refreshes, f.Conflicted = refreshes, conflicted
	return f
}

func reworkTile(t *testing.T, r insight.Rework, m insight.ReworkMetric) insight.Tile {
	t.Helper()
	for _, tl := range r.Tiles {
		if tl.Metric == insight.Metric(m) {
			return tl
		}
	}
	t.Fatalf("no tile for %s", m)
	return insight.Tile{}
}

func TestDeriveRework(t *testing.T) {
	t.Parallel()
	const (
		green   = insight.PushGreen
		red     = insight.PushRed
		pending = insight.PushPending
	)
	monday := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cur := []insight.TicketFact{
		pushed(monday.Add(time.Hour), 100, 10*time.Minute, 2, 0, green),
		pushed(monday.Add(2*time.Hour), 200, 20*time.Minute, 2, 1, red, green),
		pushed(monday.AddDate(0, 0, 8), 300, 30*time.Minute, 0, 0, red, red, green, green),
		pushed(monday.AddDate(0, 0, 9), 500, 50*time.Minute, 1, 1, green, green),
		pushed(monday.AddDate(0, 0, 10), 700, 70*time.Minute, 0, 0, pending),
	}
	r := insight.DeriveRework(cur, nil, insight.Range30d, insight.ReworkGreen)

	t.Run("tiles", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			metric insight.ReworkMetric
			want   float64
		}{
			{insight.ReworkGreen, 1.0 / 5},
			{insight.ReworkConflicted, 2.0 / 5},
			{insight.ReworkPushes, 1.0 / 5},
		}
		for _, tt := range tests {
			if got := reworkTile(t, r, tt.metric); got.Median != tt.want || got.N != 5 {
				t.Errorf("%s tile = %v over %d, want %v over 5", tt.metric, got.Median, got.N, tt.want)
			}
		}
	})

	t.Run("band columns sum to the merged tickets", func(t *testing.T) {
		t.Parallel()
		want := []insight.BandColumn{
			{Band: insight.BandFirst, Count: 3, Share: 0.6, MedianTokens: 500, MedianWall: 50 * time.Minute},
			{Band: insight.BandTwo, Count: 1, Share: 0.2, MedianTokens: 200, MedianWall: 20 * time.Minute},
			{Band: insight.BandThreePlus, Count: 1, Share: 0.2, MedianTokens: 300, MedianWall: 30 * time.Minute},
		}
		if diff := cmp.Diff(want, r.Columns); diff != "" {
			t.Errorf("Columns (-want +got):\n%s", diff)
		}
	})

	t.Run("weekly hit and miss", func(t *testing.T) {
		t.Parallel()
		next := monday.AddDate(0, 0, 7)
		tests := []struct {
			metric insight.ReworkMetric
			want   []insight.ReworkWeek
		}{
			{insight.ReworkGreen, []insight.ReworkWeek{{Start: monday, Hit: 1, Miss: 1}, {Start: next, Miss: 3}}},
			{insight.ReworkConflicted, []insight.ReworkWeek{{Start: monday, Hit: 1, Miss: 1}, {Start: next, Hit: 2, Miss: 1}}},
			{insight.ReworkPushes, []insight.ReworkWeek{{Start: monday, Hit: 2}, {Start: next, Hit: 2, Miss: 1}}},
		}
		for _, tt := range tests {
			got := insight.DeriveRework(cur, nil, insight.Range30d, tt.metric)
			if got.Selected != tt.metric {
				t.Errorf("Selected = %s, want %s", got.Selected, tt.metric)
			}
			if diff := cmp.Diff(tt.want, got.Weeks); diff != "" {
				t.Errorf("%s Weeks (-want +got):\n%s", tt.metric, diff)
			}
		}
	})

	t.Run("histogram follows the selected metric", func(t *testing.T) {
		t.Parallel()
		last := r.Histogram[len(r.Histogram)-1]
		if want := (insight.Bucket{Start: monday.AddDate(0, 0, 7), Median: 0, N: 3}); last != want {
			t.Errorf("last bucket = %+v, want %+v", last, want)
		}
		if len(reworkTile(t, r, insight.ReworkGreen).Spark) != len(r.Histogram) {
			t.Errorf("spark and histogram lengths differ")
		}
	})

	t.Run("change tone", func(t *testing.T) {
		t.Parallel()
		many := func(n int, verdicts ...insight.PushVerdict) []insight.TicketFact {
			out := make([]insight.TicketFact, n)
			for i := range out {
				out[i] = pushed(day0.Add(time.Duration(i)*time.Hour), 1, time.Minute, 2, 1, verdicts...)
			}
			return out
		}
		halfGreen := append(many(5, green), many(5, red, green)...)
		oneThreePlus := append(many(9, green), many(1, red, red, green)...)
		tests := []struct {
			name     string
			cur, pri []insight.TicketFact
			metric   insight.ReworkMetric
			want     insight.Tone
		}{
			{"green up is good", many(10, green), halfGreen, insight.ReworkGreen, insight.ToneGood},
			{"green down is bad", many(10, red, green), many(10, green), insight.ReworkGreen, insight.ToneBad},
			{"three or more up is bad", many(10, red, red, green), oneThreePlus, insight.ReworkPushes, insight.ToneBad},
			{"conflicted flat", many(10, green), many(10, green), insight.ReworkConflicted, insight.ToneFlat},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := reworkTile(t, insight.DeriveRework(tt.cur, tt.pri, insight.Range30d, tt.metric), tt.metric).Change
				if got.IsNone() || got.Unwrap().Tone != tt.want {
					t.Errorf("Change = %+v, want tone %s", got, tt.want)
				}
			})
		}
		under := insight.DeriveRework(many(9, green), many(10, red), insight.Range30d, insight.ReworkGreen)
		if got := reworkTile(t, under, insight.ReworkGreen).Change; got.IsSome() {
			t.Errorf("Change under MinChangeTickets = %+v, want none", got)
		}
	})
}
