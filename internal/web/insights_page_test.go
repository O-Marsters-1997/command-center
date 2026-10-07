package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

const goldenInsights = "testdata/insights.golden.html"

func TestInsightsPageRendersTheShellAroundTheIsland(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	got := renderPath(t, server, "/insights")
	assertGolden(t, goldenInsights, []byte(got))
}

func TestInsightsPageCarriesRepoAndFeatureScopeThrough(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	repos := []config.Repo{{Name: "cc-sandbox"}}
	server := web.NewServer(store, fixedClock(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)), repos, "")

	got := renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a")
	if !strings.Contains(got, `href="/insights?feature=feat-a"`) {
		t.Errorf("sidebar insights link does not carry the feature scope through:\n%s", got)
	}
	if !strings.Contains(got, ">feat-a<") {
		t.Errorf("breadcrumb does not show the scoped feature:\n%s", got)
	}
}

func TestInsightsPageRendersMergedTicketsAsAnSVGChart(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
	mergeInsightsTicket(t, store, "sandbox://CC-1", now)

	server := newServer(store, now)
	got := renderPath(t, server, "/insights")
	assertGolden(t, "testdata/insights_chart.golden.html", []byte(got))
}
