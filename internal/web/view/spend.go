package view

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type SpendCache struct {
	mu      sync.Mutex
	flight  singleflight.Group
	settled map[string]int
}

func NewSpendCache() *SpendCache {
	return &SpendCache{settled: make(map[string]int)}
}

func (c *SpendCache) Spend(path string) (int, error) {
	if path == "" {
		return 0, nil
	}

	c.mu.Lock()
	if tokens, ok := c.settled[path]; ok {
		c.mu.Unlock()
		return tokens, nil
	}
	c.mu.Unlock()
	value, err, _ := c.flight.Do(path, func() (any, error) {
		c.mu.Lock()
		if tokens, ok := c.settled[path]; ok {
			c.mu.Unlock()
			return tokens, nil
		}
		c.mu.Unlock()
		metrics, err := agentlog.ParseMetrics(path)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, agentlog.ErrNoParseableLines) {
			return 0, nil
		}
		if err != nil {
			return 0, fmt.Errorf("measure spend of %s: %w", path, err)
		}
		tokens := tokensOf(metrics)
		if metrics.CostUSD.IsSome() {
			c.mu.Lock()
			c.settled[path] = tokens
			c.mu.Unlock()
		}
		return tokens, nil
	})
	if err != nil {
		return 0, fmt.Errorf("read spend: %w", err)
	}
	return value.(int), nil
}

func tokensOf(metrics agentlog.RunMetrics) int {
	var tokens int
	for _, r := range metrics.Requests {
		tokens += int(r.Input + r.CacheCreate + r.CacheRead + r.Output)
	}
	return tokens
}

func applySpend(rows []Row, cache *SpendCache) error {
	for i := range rows {
		tokens, err := cache.Spend(rows[i].LogPath)
		if err != nil {
			return fmt.Errorf("read spend for %s: %w", rows[i].LogPath, err)
		}
		rows[i].SpendTokens = tokens
	}
	return nil
}

func ApplyTicketSpend(rows []Row, byURL map[string]store.BoardTicketSpend, factor float64) {
	for i := range rows {
		ts, ok := byURL[rows[i].URL]
		if !ok {
			continue
		}
		rows[i].AgentPctWeek, rows[i].FollowUpPctWeek, rows[i].SpendPctWeek =
			spend.KindPctWeek(ts.AgentUSD, ts.FollowUpUSD, factor)
		rows[i].TicketOpen = !ts.Merged
	}
}
