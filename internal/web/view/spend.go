package view

import (
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ponytail: one mutex over the whole map. Per-path locks if 25 rows ever becomes 250.
type SpendCache struct {
	mu sync.Mutex
	by map[string]*spendEntry
}

type spendEntry struct {
	acc     agentlog.Accumulator
	tokens  int
	usd     float64
	settled bool
	reads   int
}

func NewSpendCache() *SpendCache {
	return &SpendCache{by: make(map[string]*spendEntry)}
}

func (c *SpendCache) Spend(path string) (tokens int, usd float64, settled bool) {
	if path == "" {
		return 0, 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.by[path]
	if !ok {
		entry = &spendEntry{}
		c.by[path] = entry
	}
	if entry.settled {
		return entry.tokens, entry.usd, true
	}

	entry.reads++
	if err := entry.acc.Advance(path); err != nil {
		return entry.tokens, entry.usd, false
	}
	entry.tokens, entry.usd, entry.settled = entry.acc.Spend()
	return entry.tokens, entry.usd, entry.settled
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
		rows[i].TicketOpen = !ts.Merged
	}
}
