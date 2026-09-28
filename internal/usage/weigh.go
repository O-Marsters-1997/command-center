package usage

import (
	"io/fs"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// LoadRequests reads every request from every *.jsonl transcript under dir, subagent transcripts
// included, since Claude Code writes those as ordinary sibling files. A dir that does not exist
// yet yields none rather than erroring.
func LoadRequests(dir string) ([]agentlog.RequestUsage, error) {
	var requests []agentlog.RequestUsage
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // pedantic here: a missing or unreadable dir weighs zero, not a bug to surface
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		found, err := agentlog.ParseTranscriptUsage(path)
		if err != nil {
			return nil //nolint:nilerr // pedantic here: an unreadable transcript weighs zero, not a bug to surface
		}
		requests = append(requests, found...)
		return nil
	})
	return requests, err
}

// SumWeight sums the dollar cost of every request whose own timestamp falls in [start, end): a
// settled request's own exact CostUSD where it has one, agentlog.Weight's estimate otherwise.
func SumWeight(requests []agentlog.RequestUsage, start, end time.Time) float64 {
	var total float64
	for _, r := range requests {
		if r.At.Before(start) || !r.At.Before(end) {
			continue
		}
		if r.CostUSD != nil {
			total += *r.CostUSD
			continue
		}
		total += agentlog.Weight(r)
	}
	return total
}

// Weigh sums every request's Weight across every transcript under dir in [start, end); a
// single-span convenience wrapper over LoadRequests and SumWeight.
func Weigh(dir string, start, end time.Time) (float64, error) {
	requests, err := LoadRequests(dir)
	if err != nil {
		return 0, err
	}
	return SumWeight(requests, start, end), nil
}
