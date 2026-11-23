package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func insightsTicketStore(t *testing.T, now time.Time, merged bool) *storepkg.Store {
	t.Helper()
	const url = "https://github.com/acme/alpha/issues/7"
	st := openStore(t)
	storetest.FeatureTicket(t, st, url, "alpha", "feat-a")
	id := storetest.Run(t, st, storetest.RunOpts{
		TicketURL: url, Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h", StartedAt: now.Add(-3 * time.Hour),
	})
	err := st.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(id), Outcome: plan.OutcomePush, Cost: option.Some(4.20),
		Metrics: option.Some(agentlog.RunMetrics{Input: 1000, Output: 500, Tools: map[string]int{"Bash": 7}}),
		EndedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
	if merged {
		storetest.Merged(t, st, url, now.Add(-time.Hour))
	}
	return st
}

func insightsMain(t *testing.T, st *storepkg.Store, now time.Time) htmltest.Node {
	t.Helper()
	return htmltest.Parse(t, renderPath(t, newServer(st, now), "/s/acme/alpha/7?log=insights")).One(t, "main#main")
}

func insightsFigures(t *testing.T, main htmltest.Node) map[string]string {
	t.Helper()
	figures := map[string]string{}
	for _, tile := range main.Find(".pulse-tile") {
		figures[tile.One(t, ".pulse-tile-label").Text()] = tile.One(t, ".pulse-tile-figure").Text()
	}
	return figures
}

func TestSessionInsightsTab(t *testing.T) {
	t.Parallel()
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)

	t.Run("a calibrating merged ticket shows its figures without so-far labels", func(t *testing.T) {
		t.Parallel()
		got := insightsFigures(t, insightsMain(t, insightsTicketStore(t, now, true), now))
		want := map[string]string{
			"Launch to merge": "2h 00m", "Agents working": "1h 00m", "Tokens": "1.5k",
			"% of week": "Calibrating", "Tool calls": "7", "Pushes to green": "—",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET insights figures mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a withdrawn merged ticket still opens on its Insights tab", func(t *testing.T) {
		t.Parallel()
		st := insightsTicketStore(t, now, true)
		if _, err := st.WithdrawTicket(t.Context(), "https://github.com/acme/alpha/issues/7", now, true); err != nil {
			t.Fatalf("WithdrawTicket: %v", err)
		}
		renderPath(t, newServer(st, now), "/s/acme/alpha/7")
		got := insightsFigures(t, insightsMain(t, st, now))
		if got["Tokens"] != "1.5k" || got["Tool calls"] != "7" {
			t.Errorf("GET insights figures = %v, want the merged ticket's tokens and tool calls", got)
		}
	})

	t.Run("a withdrawn ticket that never merged stays not found", func(t *testing.T) {
		t.Parallel()
		st := insightsTicketStore(t, now, false)
		if _, err := st.WithdrawTicket(t.Context(), "https://github.com/acme/alpha/issues/7", now, false); err != nil {
			t.Fatalf("WithdrawTicket: %v", err)
		}
		rec := do(t, newServer(st, now), http.MethodGet, "/s/acme/alpha/7?log=insights", req{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET withdrawn unmerged session = %d, want 404", rec.Code)
		}
	})

	t.Run("an unmerged ticket labels its figures so far", func(t *testing.T) {
		t.Parallel()
		got := insightsFigures(t, insightsMain(t, insightsTicketStore(t, now, false), now))
		if got["Launch to merge"] != "—" || got["Tokens so far"] != "1.5k" || got["Tool calls so far"] != "7" {
			t.Errorf("GET insights figures = %v, want a dash for launch to merge and so-far labels", got)
		}
	})

	t.Run("the tab keeps the htmx contract and lists the runs", func(t *testing.T) {
		t.Parallel()
		main := insightsMain(t, insightsTicketStore(t, now, true), now)
		tab := main.One(t, `a.pill-tab[aria-current="page"]`)
		if got := tab.Text(); got != "Insights" {
			t.Errorf("current tab = %q, want Insights", got)
		}
		if got, _ := tab.Attr("hx-select"); got != "#main" {
			t.Errorf("Insights tab hx-select = %q, want #main", got)
		}
		if got, _ := main.One(t, "cc-insights-chart").Attr("src"); got != "/s/acme/alpha/7/insights/chart" {
			t.Errorf("chart src = %q, want the ticket chart route", got)
		}
		if rows := main.Find("table[aria-label=runs] tbody tr"); len(rows) != 1 {
			t.Errorf("runs ledger rows = %d, want 1", len(rows))
		}
	})

	t.Run("the chart route serves the cumulative spend as JSON", func(t *testing.T) {
		t.Parallel()
		body := renderPath(t, newServer(insightsTicketStore(t, now, true), now), "/s/acme/alpha/7/insights/chart")
		var got view.TicketChartWire
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("GET ticket chart: decode: %v", err)
		}
		if len(got.Steps) != 1 || got.Steps[0].Tokens != 1500 || !strings.HasPrefix(got.Steps[0].At, "2026-") {
			t.Errorf("GET ticket chart steps = %+v, want one step of 1500 tokens", got.Steps)
		}
	})
}
