package demo

import (
	"context"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
)

const (
	demoTotal   = 32 << 30
	demoBase    = 6 << 30
	claudeBase  = 400 << 20
	goplsBase   = 700 << 20
	perTurnGrow = 2 << 20
	goplsPidOff = 1_000_000
)

type Meter struct{ agent *Agent }

func NewMeter(agent *Agent) *Meter { return &Meter{agent: agent} }

var _ meter.Meter = (*Meter)(nil)

func (m *Meter) Read(_ context.Context, pgids []int) (meter.Reading, error) {
	m.agent.mu.Lock()
	defer m.agent.mu.Unlock()
	r := meter.Reading{Total: demoTotal, Used: demoBase}
	for _, pgid := range slices.Sorted(maps.Keys(m.agent.runs)) {
		run := m.agent.runs[pgid]
		if run.finished {
			continue
		}
		grown := int64(run.turns) * perTurnGrow
		procs := []meter.Process{
			{Pgid: pgid, Pid: pgid, Name: "claude", Args: "claude --print " + run.issue.ID, RSS: claudeBase + grown},
			{Pgid: pgid, Pid: pgid + goplsPidOff, Name: "gopls", Args: "gopls serve", RSS: goplsBase + grown},
		}
		for _, p := range procs {
			r.Used += p.RSS
			if slices.Contains(pgids, pgid) {
				r.Procs = append(r.Procs, p)
			}
		}
	}
	return r, nil
}
