package spend

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

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
