package plan

import (
	"slices"
	"time"
)

const CommandDeadline = 10 * time.Minute

type Caps struct {
	Agents   int
	Commands int
}

type Load struct {
	Agents   int
	Commands int
}

func (l Load) charge(command bool) Load {
	if command {
		l.Commands++
	} else {
		l.Agents++
	}
	return l
}

func (l Load) fits(command bool, caps Caps) bool {
	if command {
		return l.Commands < caps.Commands
	}
	return l.Agents < caps.Agents
}

func (s Snapshot) Overdue() []RunID {
	var ids []RunID
	for _, run := range s.runs {
		started := run.ProcStartedAt
		live := run.Pgid.IsSome() && run.Outcome.IsNone()
		if run.Kind == RunKindCommand && live && started.IsSome() && s.now.Sub(started.Unwrap()) > CommandDeadline {
			ids = append(ids, run.ID)
		}
	}
	slices.Sort(ids)
	return ids
}
