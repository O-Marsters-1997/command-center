package spend_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

// TestSumWeightPrefersASettledRequestsOwnExactCost covers a settled run's own reported
// total_cost_usd taking priority over agentlog.Weight's token-price estimate, which the token
// counts here would put nowhere near.
func TestSumWeightPrefersASettledRequestsOwnExactCost(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exact := 123.45
	settled := agentlog.RequestUsage{At: at, Model: "claude-sonnet-5", Input: 1, CostUSD: &exact}

	got := spend.SumWeight([]agentlog.RequestUsage{settled}, at, at.Add(time.Minute))
	if got != exact {
		t.Errorf("SumWeight = %v; want the settled request's own exact cost %v", got, exact)
	}
}
