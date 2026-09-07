package cc

import (
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// ponytail: one mutex over the whole map. Per-path locks if 25 rows ever becomes 250.
type spendCache struct {
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

func newSpendCache() *spendCache {
	return &spendCache{by: make(map[string]*spendEntry)}
}

func (c *spendCache) Spend(path string) (tokens int, usd float64, settled bool) {
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

func applySpend(rows []row, cache *spendCache) {
	for i := range rows {
		rows[i].SpendTokens, rows[i].SpendUSD, rows[i].SpendSettled = cache.Spend(rows[i].LogPath)
	}
}
