package agentlog

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Accumulator reads a run's log forward from where it last stopped, so a caller polling spend
// every tick pays for the bytes appended since rather than the whole file.
type Accumulator struct {
	offset int64
	in     int
	out    int
	// requests dedupes usage: the CLI repeats one request's usage on every content block that
	// request emitted, and a subagent's blocks can land between two of its parent's.
	requests map[string]struct{}
	settled  *Result
}

// Advance reads the whole lines appended to path since the last call. A log that will not open
// yet is no lines rather than an error — a run's row exists a beat before its file does.
func (a *Accumulator) Advance(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open agent log %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(a.offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek agent log %s to %d: %w", path, a.offset, err)
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read agent log %s: %w", path, err)
		}
		a.offset += int64(len(line))
		a.take(line)
	}
}

// Spend is the run's cost so far: tokens always, dollars only once a result has landed. The
// dollars are the agent's own total_cost_usd, so this package holds no model prices.
func (a *Accumulator) Spend() (tokens int, usd float64, settled bool) {
	if a.settled == nil {
		return a.in + a.out, 0, false
	}
	return a.in + a.out, a.settled.CostUSD, true
}

func (a *Accumulator) take(line []byte) {
	parsed, err := decode(line)
	if err != nil {
		return
	}
	if parsed.Type == "result" {
		result := parsed.result()
		a.settled = &result
		return
	}
	if parsed.Type != "assistant" {
		return
	}
	if _, seen := a.requests[parsed.RequestID]; seen {
		return
	}
	if a.requests == nil {
		a.requests = make(map[string]struct{})
	}
	a.requests[parsed.RequestID] = struct{}{}

	spent := parsed.Message.Usage
	a.in += spent.Input + spent.CacheCreate + spent.CacheRead
	a.out += spent.Output
}
