package web_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestInsightsChart(t *testing.T) {
	t.Parallel()
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)
	const url = "https://github.com/acme/alpha/issues/7"

	store := openStore(t)
	storetest.FeatureTicket(t, store, url, "alpha", "feat-a")
	id := storetest.Run(t, store, storetest.RunOpts{
		TicketURL: url, Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h", StartedAt: now.Add(-3 * time.Hour),
	})
	err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(id), Outcome: plan.OutcomePush, Cost: option.Some(4.20),
		Metrics: option.Some(agentlog.RunMetrics{Input: 1000, Output: 500, Tools: map[string]int{"Bash": 7}}),
		EndedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
	storetest.Merged(t, store, url, now.Add(-time.Hour))
	server := newServer(store, now)

	for _, path := range []string{
		"/insights/chart", "/insights/chart?metric=pct", "/insights/chart?metric=tools",
		"/insights/chart?metric=wall&view=week&range=7d",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var got view.ChartWire
			if err := json.Unmarshal([]byte(renderPath(t, server, path)), &got); err != nil {
				t.Fatalf("GET %s: decode: %v", path, err)
			}
			if len(got.Dots) != 1 || got.Dots[0].Href != "/s/acme/alpha/7?log=insights" || got.Dots[0].Ticket != "#7" {
				t.Errorf("GET %s dots = %+v, want one dot linking to the session page", path, got.Dots)
			}
			if len(got.Buckets) == 0 || got.Unit == "" {
				t.Errorf("GET %s = %+v, want buckets and a unit", path, got)
			}
		})
	}
}
