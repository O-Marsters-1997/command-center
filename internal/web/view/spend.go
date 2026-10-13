package view

import (
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type SpendCache struct {
	mu      sync.Mutex
	settled map[string]int
}

func NewSpendCache() *SpendCache {
	return &SpendCache{settled: make(map[string]int)}
}

func (c *SpendCache) Spend(path string) int {
	if path == "" {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if tokens, ok := c.settled[path]; ok {
		return tokens
	}

	metrics, err := agentlog.ParseMetrics(path)
	if err != nil {
		return 0
	}
	tokens := tokensOf(metrics)
	if metrics.Settled {
		c.settled[path] = tokens
	}
	return tokens
}

func tokensOf(metrics agentlog.RunMetrics) int {
	var tokens int
	for _, r := range metrics.Requests {
		tokens += int(r.InputTokens + r.CacheCreationTokens + r.CacheReadTokens + r.OutputTokens)
	}
	return tokens
}

func applySpend(rows []Row, cache *SpendCache) {
	for i := range rows {
		rows[i].SpendTokens = cache.Spend(rows[i].LogPath)
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
		rows[i].TicketOpen = !ts.Merged
	}
}
