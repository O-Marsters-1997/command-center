// Package verdict evaluates one repo's boolean check predicate over a normalised snapshot —
// never gh's raw JSON. It declares its own CheckState rather than importing gh's;
// api_test.go enforces the import purity.
package verdict

import (
	"fmt"
	"maps"
	"strings"
	"time"
)

// CheckState is one gating check's outcome. The zero value, Pending, doubles as "absent from the
// rollup", since a missing map key already returns it.
type CheckState int

const (
	Pending CheckState = iota
	Success
	Failure
	Skipped
)

type Verdict int

const (
	Checking Verdict = iota
	ReviewMe
	NeedsYou
	BaseMoved
	WaitingOnProducerDeploy
)

// Label names the result for comparison against the last recorded one. A red leaf under NeedsYou
// is ci_failed; a nil result has no label.
func (r *Result) Label() string {
	if r == nil {
		return ""
	}
	switch r.Verdict {
	case BaseMoved:
		return "base_moved"
	case WaitingOnProducerDeploy:
		return "waiting_on_producer_deploy"
	case ReviewMe:
		return "review_me"
	case NeedsYou:
		if len(r.RedLeaves) > 0 {
			return "ci_failed"
		}
		return "needs_you"
	default:
		return "checking"
	}
}

// BoundedWait is how long a still-pending predicate is tolerated before Evaluate gives up on it,
// clocked over ticks whose observe phase succeeded so a GitHub outage cannot walk every row to needs_you.
const BoundedWait = 10 * time.Minute

// Predicate is the boolean check-config grammar: all_of, any_of, not, success, skipped,
// absent_ok, plus Author for a PR's author, which is not a check-run.
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

// Result is Evaluate's answer plus the sentence the page renders. RedLeaves is empty unless
// Verdict is NeedsYou because a leaf resolved red, rather than the bounded wait elapsing.
type Result struct {
	Verdict   Verdict
	Reason    string
	RedLeaves []string
}

type triState int

const (
	pending triState = iota
	green
	red
)

// Evaluate resolves p against in. A foreign-SHA or absent rollup is never green, and the
// stacked-base expiry outranks red. A needs_you verdict is re-resolved with the compat check
// forced green: review-me there means the compat check was the sole red one.
func Evaluate(p Predicate, in Input) Result {
	if in.StackedBase && !in.BaseSHAMatch {
		return Result{Verdict: BaseMoved, Reason: "base moved: the parent advanced past what this branch was cut from"}
	}
	if !in.HeadOidMatch {
		in.Checks = nil
	}

	result := evaluate(p, in)
	if result.Verdict != NeedsYou || in.CompatCheck == "" {
		return result
	}

	in.Checks = forcedGreen(in.Checks, in.CompatCheck)
	switch forced := evaluate(p, in); forced.Verdict {
	case ReviewMe:
		return Result{Verdict: WaitingOnProducerDeploy, Reason: "every required check passed except the compat check"}
	case Checking:
		return forced
	default:
		return result
	}
}

func evaluate(p Predicate, in Input) Result {
	state, redLeaves := resolve(p, in)
	switch state {
	case red:
		return Result{
			Verdict:   NeedsYou,
			Reason:    fmt.Sprintf("required check failed: %s", strings.Join(redLeaves, ", ")),
			RedLeaves: redLeaves,
		}
	case green:
		if !in.ConfigHashOK {
			return Result{Verdict: Checking, Reason: "check config changed"}
		}
		return Result{Verdict: ReviewMe, Reason: "every required check passed"}
	default:
		if waited(in) {
			return Result{Verdict: NeedsYou, Reason: "no matching rollup within the wait"}
		}
		return Result{Verdict: Checking, Reason: "waiting on checks"}
	}
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
	default:
		return pending
	}
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
	switch got {
	case want:
		return green
	case Pending:
		return pending
	default:
		return red
	}
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
	default:
		return red
	}
}

func author(got, want string) triState {
	if got == want {
		return green
	}
	return red
}
