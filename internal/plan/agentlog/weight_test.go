package agentlog_test

import (
	"math"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func TestWeight(t *testing.T) {
	t.Parallel()

	t.Run("matches run27's total cost", func(t *testing.T) {
		t.Parallel()
		const run27TotalCostUSD = 8.2887992
		requests, err := agentlog.ParseTranscriptUsage("testdata/run27.jsonl")
		if err != nil || len(requests) != 1 {
			t.Fatalf("ParseTranscriptUsage(run27.jsonl) = %d entries, %v, want one", len(requests), err)
		}

		got := agentlog.Weight(requests[0])
		if off := math.Abs(got-run27TotalCostUSD) / run27TotalCostUSD; off > 0.01 {
			t.Errorf("Weight(run27) = %v, want within 1%% of %v", got, run27TotalCostUSD)
		}
	})

	t.Run("prices by model tier, unknown as sonnet", func(t *testing.T) {
		t.Parallel()
		weigh := func(model string) float64 {
			return agentlog.Weight(agentlog.RequestUsage{Model: model, Input: 1_000_000})
		}
		haiku, sonnet, opus := weigh("claude-haiku-4-5-20251001"), weigh("claude-sonnet-5"), weigh("claude-opus-5-5")
		if !(haiku < sonnet && sonnet < opus) {
			t.Errorf("Weight(haiku, sonnet, opus) = %v, %v, %v, want ascending", haiku, sonnet, opus)
		}
		if unknown := weigh("some-future-model"); unknown != sonnet {
			t.Errorf("Weight(some-future-model) = %v, want the sonnet price %v", unknown, sonnet)
		}
	})
}
