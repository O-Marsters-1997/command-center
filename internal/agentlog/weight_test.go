package agentlog_test

import (
	"math"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// TestWeightMatchesRun27sTotalCostUSD covers CC-313's acceptance criterion: priced by Weight,
// run27's own settled usage must agree with the total_cost_usd its result line reports, to
// within 1%, or the fit downstream would be trained on a systematically wrong dollar figure.
func TestWeightMatchesRun27sTotalCostUSD(t *testing.T) {
	t.Parallel()

	const run27TotalCostUSD = 8.2887992

	requests, err := agentlog.ParseTranscriptUsage("testdata/run27.jsonl")
	if err != nil {
		t.Fatalf("ParseTranscriptUsage: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("run27 is a settled run, want exactly one entry, got %d", len(requests))
	}

	got := agentlog.Weight(requests[0])
	diff := math.Abs(got-run27TotalCostUSD) / run27TotalCostUSD
	if diff > 0.01 {
		t.Errorf("Weight(run27) = %v, want within 1%% of %v (off by %.2f%%)", got, run27TotalCostUSD, diff*100)
	}
}

func TestWeightPricesByModelTier(t *testing.T) {
	t.Parallel()

	usage := agentlog.RequestUsage{Input: 1_000_000}
	sonnet := agentlog.Weight(agentlog.RequestUsage{Model: "claude-sonnet-5", Input: usage.Input})
	opus := agentlog.Weight(agentlog.RequestUsage{Model: "claude-opus-5-5", Input: usage.Input})
	haiku := agentlog.Weight(agentlog.RequestUsage{Model: "claude-haiku-4-5-20251001", Input: usage.Input})

	if !(haiku < sonnet && sonnet < opus) {
		t.Errorf("want haiku < sonnet < opus, got haiku=%v sonnet=%v opus=%v", haiku, sonnet, opus)
	}

	unknown := agentlog.Weight(agentlog.RequestUsage{Model: "some-future-model", Input: usage.Input})
	if unknown != sonnet {
		t.Errorf("unknown model = %v, want the sonnet fallback %v", unknown, sonnet)
	}
}
