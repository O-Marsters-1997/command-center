package verdict

import (
	"fmt"
	"maps"
	"strings"
	"time"
)

// CheckState is one gating check's outcome; absent rollup entries are Pending.
type CheckState int

const (
	Pending CheckState = iota
	Success
	Failure
	Skipped
)

type Kind int

const (
	KindUnknown Kind = iota
	Checking
	ReviewMe
	NeedsYou
	CIFailed
	BaseMoved
	WaitingOnProducerDeploy
)

var kindNames = map[Kind]string{
	Checking:                "checking",
	ReviewMe:                "review_me",
	NeedsYou:                "needs_you",
	CIFailed:                "ci_failed",
	BaseMoved:               "base_moved",
	WaitingOnProducerDeploy: "waiting_on_producer_deploy",
}

func (k Kind) String() string {
	return kindNames[k]
}

// BoundedWait limits how long a predicate may remain pending.
// A GitHub outage cannot advance the wait because only successful observations count.
const BoundedWait = 10 * time.Minute

type Predicate struct {
	AllOf    []Predicate `toml:"all_of"`
	AnyOf    []Predicate `toml:"any_of"`
	Not      *Predicate  `toml:"not"`
	Success  string      `toml:"success"`
	Skipped  string      `toml:"skipped"`
	AbsentOK string      `toml:"absent_ok"`
	Author   string      `toml:"author"`
}

func (p Predicate) IsZero() bool {
	return len(p.AllOf) == 0 && len(p.AnyOf) == 0 && p.Not == nil &&
		p.Success == "" && p.Skipped == "" && p.AbsentOK == "" && p.Author == ""
}

type Input struct {
	Checks       map[string]CheckState
	HeadOidMatch bool
	StackedBase  bool
	BaseSHAMatch bool
	ConfigHashOK bool
	PushedAt     time.Time
	Now          time.Time
	AuthorLogin  string
	CompatCheck  string
}

type Result struct {
	Kind      Kind
	Reason    string
	RedLeaves []string
}

type triState int

const (
	pending triState = iota
	green
	red
)

func Evaluate(p Predicate, in Input) Result {
	if in.StackedBase && !in.BaseSHAMatch {
		return Result{Kind: BaseMoved, Reason: "base moved: the parent advanced past what this branch was cut from"}
	}
	if !in.HeadOidMatch {
		in.Checks = nil
	}

	result := evaluate(p, in)
	if (result.Kind != NeedsYou && result.Kind != CIFailed) || in.CompatCheck == "" {
		return result
	}

	in.Checks = forcedGreen(in.Checks, in.CompatCheck)
	switch forced := evaluate(p, in); forced.Kind {
	case ReviewMe:
		return Result{Kind: WaitingOnProducerDeploy, Reason: "every required check passed except the compat check"}
	case Checking:
		return forced
	case KindUnknown, NeedsYou, CIFailed, BaseMoved, WaitingOnProducerDeploy:
	}
	return result
}

func evaluate(p Predicate, in Input) Result {
	state, redLeaves := resolve(p, in)
	switch state {
	case red:
		return Result{
			Kind:      CIFailed,
			Reason:    fmt.Sprintf("required check failed: %s", strings.Join(redLeaves, ", ")),
			RedLeaves: redLeaves,
		}
	case green:
		if !in.ConfigHashOK {
			return Result{Kind: Checking, Reason: "check config changed"}
		}
		return Result{Kind: ReviewMe, Reason: "every required check passed"}
	case pending:
	}
	if waited(in) {
		return Result{Kind: NeedsYou, Reason: "no matching rollup within the wait"}
	}
	return Result{Kind: Checking, Reason: "waiting on checks"}
}

func forcedGreen(checks map[string]CheckState, name string) map[string]CheckState {
	out := make(map[string]CheckState, len(checks)+1)
	maps.Copy(out, checks)
	out[name] = Success
	return out
}

func waited(in Input) bool {
	return !in.PushedAt.IsZero() && in.Now.Sub(in.PushedAt) >= BoundedWait
}

func resolve(p Predicate, in Input) (triState, []string) {
	switch {
	case len(p.AllOf) > 0:
		return allOf(p.AllOf, in)
	case len(p.AnyOf) > 0:
		return anyOf(p.AnyOf, in)
	case p.Not != nil:
		t, _ := resolve(*p.Not, in)
		return not(t), nil
	default:
		return leaf(p, in)
	}
}

func allOf(ps []Predicate, in Input) (triState, []string) {
	result := green
	var redLeaves []string
	for _, p := range ps {
		state, leaves := resolve(p, in)
		switch state {
		case red:
			result = red
			redLeaves = append(redLeaves, leaves...)
		case pending:
			if result == green {
				result = pending
			}
		case green:
		}
	}
	return result, redLeaves
}

func anyOf(ps []Predicate, in Input) (triState, []string) {
	result := red
	var redLeaves []string
	for _, p := range ps {
		state, leaves := resolve(p, in)
		switch state {
		case green:
			return green, nil
		case pending:
			result = pending
		case red:
			redLeaves = append(redLeaves, leaves...)
		}
	}
	if result != red {
		return result, nil
	}
	return red, redLeaves
}

func not(t triState) triState {
	switch t {
	case green:
		return red
	case red:
		return green
	case pending:
	}
	return pending
}

func leaf(p Predicate, in Input) (triState, []string) {
	switch {
	case p.Success != "":
		return leafResult(requireConclusion(in.Checks[p.Success], Success), p.Success)
	case p.Skipped != "":
		return leafResult(requireConclusion(in.Checks[p.Skipped], Skipped), p.Skipped)
	case p.AbsentOK != "":
		return leafResult(absentOK(in.Checks, p.AbsentOK, in), p.AbsentOK)
	case p.Author != "":
		return leafResult(author(in.AuthorLogin, p.Author), p.Author)
	default:
		return pending, nil
	}
}

func leafResult(t triState, name string) (triState, []string) {
	if t != red {
		return t, nil
	}
	return red, []string{name}
}

func requireConclusion(got, want CheckState) triState {
	if got == want {
		return green
	}
	if got == Pending {
		return pending
	}
	return red
}

func absentOK(checks map[string]CheckState, name string, in Input) triState {
	got, present := checks[name]
	if !present {
		if waited(in) {
			return green
		}
		return pending
	}
	switch got {
	case Success, Skipped:
		return green
	case Pending:
		return pending
	case Failure:
	}
	return red
}

func author(got, want string) triState {
	if got == want {
		return green
	}
	return red
}
