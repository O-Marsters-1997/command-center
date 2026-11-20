package plan

import (
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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

type VerifyFact struct {
	Run       RunID
	Tip       SHA
	Alive     bool
	Outcome   option.Option[Outcome]
	StartedAt time.Time
	LogPath   string
}

type Verify struct {
	Ticket TicketURL
	Tip    SHA
}

type VerifyPhase int

const (
	VerifyNone VerifyPhase = iota
	VerifyWaiting
	VerifyRunning
	VerifyFailed
)

func (f VerifyFact) overdue(now time.Time) bool {
	return f.Alive && now.Sub(f.StartedAt) > CommandDeadline
}

func (r Rules) verifyPhase(t Ticket, in Input, lastPush option.Option[PushRow]) VerifyPhase {
	summary, ok := in.Runs[t.URL]
	if !r.Verify[t.Repo] || !ok || summary.Outcome != option.Some(OutcomePush) {
		return VerifyNone
	}
	tip := in.Obs.LocalTips[t.Ref()]
	fact, has := in.Verifies[t.URL]
	switch {
	case has && fact.overdue(in.Now):
		return VerifyFailed
	case has && fact.Alive:
		return VerifyRunning
	case tip == "":
		return VerifyNone
	case has && fact.Tip == tip && fact.Outcome == option.Some(OutcomePassed):
		return VerifyNone
	case has && fact.Tip == tip:
		return VerifyFailed
	case lastPush.IsSome() && lastPush.Unwrap().PushedTip == tip:
		return VerifyNone
	case in.Refreshes[t.URL].Advanced || summary.Kind == RunKindResolve:
		return VerifyWaiting
	}
	return VerifyNone
}

func (s Snapshot) VerifyAfter(spawnedCommands int) []Verify {
	load := s.load
	load.Commands += spawnedCommands
	var out []Verify
	for _, v := range s.verifies {
		if !load.fits(true, s.caps) {
			break
		}
		out = append(out, v)
		load = load.charge(true)
	}
	return out
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
	for _, fact := range s.verifyFacts {
		if fact.overdue(s.now) {
			ids = append(ids, fact.Run)
		}
	}
	slices.Sort(ids)
	return ids
}
