package metertest

import (
	"context"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
)

type Meter struct {
	Total, Used int64
	Procs       []meter.Process
}

var _ meter.Meter = Meter{}

func (m Meter) Read(_ context.Context, pgids []int) (meter.Reading, error) {
	r := meter.Reading{Total: m.Total, Used: m.Used}
	for _, p := range m.Procs {
		if slices.Contains(pgids, p.Pgid) {
			r.Procs = append(r.Procs, p)
		}
	}
	return r, nil
}
