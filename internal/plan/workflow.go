package plan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type StepName string

type Step struct {
	Name    StepName
	Command bool     `json:",omitempty"`
	Model   string   `json:",omitempty"`
	Tools   []string `json:",omitempty"`
	Prompt  string   `json:",omitempty"`
	Writes  bool     `json:",omitempty"`
	OnFail  bool     `json:",omitempty"`
}

type Workflow struct {
	Name         string
	Hash         string
	Steps        []Step
	MaxFixRounds int `json:",omitempty"`
}

type StepRun struct {
	ID      RunID
	Step    StepName
	Alive   bool
	Outcome option.Option[Outcome]
	Handoff option.Option[string]
}

type NextKind int

const (
	NextRun NextKind = iota
	NextDone
	NextPark
)

type Next struct {
	Kind   NextKind
	Step   option.Option[Step]
	Round  int
	Reason Reason
}

func (w Workflow) split(hasVerify bool) (forward []Step, fix option.Option[Step]) {
	fix = option.None[Step]()
	for _, s := range w.Steps {
		switch {
		case s.OnFail:
			fix = option.Some(s)
		case s.Command && !hasVerify:
		default:
			forward = append(forward, s)
		}
	}
	return forward, fix
}

func (w Workflow) First(hasVerify bool) option.Option[Step] {
	forward, _ := w.split(hasVerify)
	if len(forward) == 0 {
		return option.None[Step]()
	}
	return option.Some(forward[0])
}

func (w Workflow) Pass(runs []StepRun, hasVerify bool) []StepRun {
	forward, _ := w.split(hasVerify)
	if len(forward) == 0 {
		return nil
	}
	return fromLatest(runs, forward[0].Name)
}

func (w Workflow) Next(pass []StepRun, hasVerify bool) Next {
	forward, fix := w.split(hasVerify)
	if len(forward) == 0 {
		return Next{Kind: NextDone}
	}
	pass = fromLatest(pass, forward[0].Name)
	if len(pass) == 0 {
		return runStep(forward[0], 0)
	}
	if slices.ContainsFunc(pass, func(r StepRun) bool { return r.Alive }) {
		return Next{Kind: NextRun}
	}

	rounds := 0
	var failed StepName
	for _, r := range pass {
		if fix.IsSome() && r.Step == fix.Unwrap().Name {
			rounds++
		} else {
			failed = r.Step
		}
	}
	last := pass[len(pass)-1]
	outcome := last.Outcome.UnwrapOr(OutcomeFailed)

	if fix.IsSome() && last.Step == fix.Unwrap().Name {
		if outcome != OutcomePassed {
			return park(last.Step, outcome)
		}
		return rerun(forward, failed, rounds)
	}

	i := slices.IndexFunc(forward, func(s Step) bool { return s.Name == last.Step })
	if i < 0 {
		return unknownStep(last.Step)
	}
	final := i == len(forward)-1
	switch {
	case final && outcome == OutcomePush:
		return Next{Kind: NextDone}
	case outcome == OutcomePassed && !final:
		return runStep(forward[i+1], rounds)
	case outcome == OutcomeFailed && !final && fix.IsSome() && rounds < w.MaxFixRounds:
		return runStep(fix.Unwrap(), rounds+1)
	case outcome == OutcomeFailed && !final && fix.IsSome():
		return Next{Kind: NextPark, Reason: Reason(fmt.Sprintf(
			"step %s still failing after %d fix rounds", last.Step, rounds))}
	}
	return park(last.Step, outcome)
}

func fromLatest(pass []StepRun, first StepName) []StepRun {
	for i := len(pass) - 1; i >= 0; i-- {
		if pass[i].Step == first {
			return pass[i:]
		}
	}
	return pass
}

func rerun(forward []Step, name StepName, round int) Next {
	i := slices.IndexFunc(forward, func(s Step) bool { return s.Name == name })
	if i < 0 {
		return unknownStep(name)
	}
	return runStep(forward[i], round)
}

func runStep(s Step, round int) Next {
	return Next{Kind: NextRun, Step: option.Some(s), Round: round}
}

func park(step StepName, o Outcome) Next {
	return Next{Kind: NextPark, Reason: Reason(fmt.Sprintf("step %s ended %s", step, o))}
}

func unknownStep(step StepName) Next {
	return Next{Kind: NextPark, Reason: Reason(fmt.Sprintf("step %s is not in the workflow", step))}
}

const FindingsMarker = "FINDINGS"

type StepEvidence struct {
	ExitCode option.Option[int]
	Commits  int
	Handoff  option.Option[string]
}

func (s Step) StepOutcome(e StepEvidence) Outcome {
	var ok bool
	switch {
	case s.Command:
		ok = e.ExitCode.IsSome() && e.ExitCode.Unwrap() == 0
	case s.Writes:
		handoff := strings.TrimSpace(e.Handoff.UnwrapOr(""))
		ok = handoff != "" && !strings.HasPrefix(handoff, FindingsMarker)
	default:
		ok = e.Commits > 0
	}
	if ok {
		return OutcomePassed
	}
	return OutcomeFailed
}

func ComposeStep(t Ticket, step Step, pass []StepRun) string {
	var b strings.Builder
	b.WriteString(step.Prompt)
	fmt.Fprintf(&b, "\n\n## Ticket\n\n%s", t.URL)
	if t.LaunchContext != "" {
		b.WriteString("\n\n## Launch context\n\n" + t.LaunchContext)
	}
	for _, r := range pass {
		if r.Handoff.IsSome() {
			fmt.Fprintf(&b, "\n\n## Handoff from %s\n\n%s", r.Step, r.Handoff.Unwrap())
		}
	}
	return b.String()
}
