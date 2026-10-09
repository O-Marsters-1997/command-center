package view

import (
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type SpendCache struct {
	mu      sync.Mutex
	settled map[string]runSpend
}

type runSpend struct {
	tokens int
	usd    float64
}

func NewSpendCache() *SpendCache {
	return &SpendCache{settled: make(map[string]runSpend)}
}

func (c *SpendCache) Spend(path string) (tokens int, usd float64, settled bool) {
	if path == "" {
		return 0, 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if spent, ok := c.settled[path]; ok {
		return spent.tokens, spent.usd, true
	}

	metrics, err := agentlog.ParseMetrics(path)
	if err != nil {
		return 0, 0, false
	}
	spent := spendOf(metrics)
	if metrics.Settled {
		c.settled[path] = spent
	}
	return spent.tokens, spent.usd, metrics.Settled
}

func spendOf(metrics agentlog.RunMetrics) runSpend {
	var spent runSpend
	for _, r := range metrics.Requests {
		spent.tokens += int(r.InputTokens + r.CacheCreationTokens + r.CacheReadTokens + r.OutputTokens)
	}
	if metrics.Settled && metrics.CostUSD != nil {
		spent.usd = *metrics.CostUSD
	}
	return spent
}

func applySpend(rows []Row, cache *SpendCache) {
	for i := range rows {
		rows[i].SpendTokens, rows[i].SpendUSD, rows[i].SpendSettled = cache.Spend(rows[i].LogPath)
	}
}

func applyTicketSpend(rows []Row, byURL map[string]store.BoardTicketSpend, factor float64) {
	for i := range rows {
		ts, ok := byURL[rows[i].URL]
		if !ok {
			continue
		}
		rows[i].AgentPctWeek, rows[i].ResolvePctWeek, rows[i].FollowUpPctWeek, rows[i].SpendPctWeek =
			spend.KindPctWeek(ts.AgentUSD, ts.ResolveUSD, ts.FollowUpUSD, factor)
		rows[i].SpendPctWeek += spend.PctWeek(ts.ExploreUSD, factor)
		rows[i].TicketOpen = !ts.Merged
	}
}
