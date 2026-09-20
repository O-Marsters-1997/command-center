package cc_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
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
	repos := []cc.Repo{{Name: "cc-sandbox"}}
	server := cc.NewServer(store, fixedClock(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)), repos, "")

	got := renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a")
	if !strings.Contains(got, `href="/insights?feature=feat-a"`) {
		t.Errorf("sidebar insights link does not carry the feature scope through:\n%s", got)
	}
	if !strings.Contains(got, ">feat-a<") {
		t.Errorf("breadcrumb does not show the scoped feature:\n%s", got)
	}
}

func TestInsightsPageServesInsightsJSONFromTheSameOrigin(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	got := renderPath(t, server, "/insights")
	if !strings.Contains(got, "<cc-insights>") {
		t.Fatalf("no cc-insights island in the page:\n%s", got)
	}
	if !strings.Contains(got, `src="/assets/dist/insights.js"`) {
		t.Errorf("no script tag loading the built island:\n%s", got)
	}

	if resp, _ := fetchInsights(t, server, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /insights.json = %d, want 200", resp.StatusCode)
	}
}
