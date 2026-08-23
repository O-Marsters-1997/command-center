// Package verdict evaluates one repo's boolean check predicate over a normalised snapshot —
// never gh's raw JSON (docs/command-centre-design.md § 11 inv. 11). Like internal/plan, it imports
// nothing outside the standard library (issue #2 AC12: internal/gh execs, so this package
// declares its own CheckState rather than importing gh.CheckState); api_test.go enforces it.
package verdict

import "time"

// CheckState is one gating check's outcome, as this package needs it. The zero value is
// Pending, which doubles as "absent from the rollup" -- a map lookup by a missing key already
// returns it, so a leaf never has to special-case presence unless it specifically cares (see
// AbsentOK's leniency).
type CheckState int

const (
	Pending CheckState = iota
	Success
	Failure
	Skipped
)

// Verdict is Evaluate's headline answer -- one of the three states the PRD names for a pushed
// PR (docs/prd-command-centre.md § The states): review me, needs you, checking.
type Verdict int

const (
	Checking Verdict = iota
	ReviewMe
	NeedsYou
)

func (v Verdict) String() string {
	switch v {
	case ReviewMe:
		return "review_me"
	case NeedsYou:
		return "needs_you"
	default:
		return "checking"
	}
}

// BoundedWait is how long a still-pending predicate is tolerated before Evaluate gives up on it
// (an absent absent_ok check, or no rollup at all) -- clocked over ticks whose observe phase
// succeeded only, never wall clock, so a GitHub outage cannot walk every in-flight row to
// needs_you the moment it ends (docs/command-centre-design.md § 11 inv. 11). The PRD leaves the
// number itself an open question; ten minutes is this package's one knob, moved by a human if
// it proves wrong.
const BoundedWait = 10 * time.Minute

// Predicate is the boolean check-config grammar (docs/command-centre-design.md § 8): all_of / any_of
// / not / success / skipped / absent_ok, plus Author -- the non-check escape hatch services'
// Linear-branch predicate needs for its dependabot arm, since a PR's author is not a check-run.
// Exactly one leaf field, or one of AllOf/AnyOf/Not, is set on any given node; that shape is
// parsed straight out of [repo.checks] by encoding/toml, not asserted here.
type Predicate struct {
	AllOf    []Predicate `toml:"all_of"`
	AnyOf    []Predicate `toml:"any_of"`
	Not      *Predicate  `toml:"not"`
	Success  string      `toml:"success"`
	Skipped  string      `toml:"skipped"`
	AbsentOK string      `toml:"absent_ok"`
	Author   string      `toml:"author"`
}

// IsZero reports whether p carries no rule at all -- an unconfigured [repo.checks], which the
// caller reads as "no predicate to evaluate" rather than a predicate that vacuously passes.
func (p Predicate) IsZero() bool {
	return len(p.AllOf) == 0 && len(p.AnyOf) == 0 && p.Not == nil &&
		p.Success == "" && p.Skipped == "" && p.AbsentOK == "" && p.Author == ""
}

// Input is everything Evaluate needs, honestly enumerated (docs/command-centre-design.md § 8):
// the reduced check map, whether the rollup belongs to the commit the app pushed, the stacked-
// base check (unscoped, main's own tip moves would make every root row look like its base
// moved), the config-hash match, the bounded wait's two endpoints, and the PR author for the
// dependabot arm.
type Input struct {
	Checks       map[string]CheckState
	HeadOidMatch bool // the rollup's head == the tip the app pushed
	StackedBase  bool // false for every Phase 1 row (stacking = false)
	BaseSHAMatch bool // only consulted when StackedBase
	ConfigHashOK bool // sha256(.mergify.yml) still matches the predicate it was written against
	PushedAt     time.Time
	Now          time.Time // the bounded wait's other endpoint; not wall clock (see BoundedWait)
	AuthorLogin  string    // the dependabot arm of services' Linear branch check
}

// Result is Evaluate's answer plus the sentence the page renders alongside it.
type Result struct {
	Verdict Verdict
	Reason  string
}

// triState is a predicate node's resolution before Evaluate turns it into a Result: still
// waiting, definitely satisfied, or definitely not -- the third value pending needs, since a
// boolean can't tell "not yet" from "no".
type triState int

const (
	pending triState = iota
	green
	red
)

// Evaluate resolves p against in and reports the verdict. Semantics that have to hold (docs/
// command-centre-design.md § 11 inv. 11): an empty rollup, an absent rollup, or one belonging to
// another commit is never green -- HeadOidMatch being false discards in.Checks entirely, so a
// foreign-SHA rollup reads exactly like no rollup at all. A resolved-red predicate is needs_you
// outright, config-hash and stacked-base staleness notwithstanding: those two only ever
// downgrade a green result, never excuse a real red one.
func Evaluate(p Predicate, in Input) Result {
	if !in.HeadOidMatch {
		in.Checks = nil
	}

	switch resolve(p, in) {
	case red:
		return Result{NeedsYou, "a required check failed"}
	case green:
		if in.StackedBase && !in.BaseSHAMatch {
			return Result{Checking, "base moved"}
		}
		if !in.ConfigHashOK {
			return Result{Checking, "check config changed"}
		}
		return Result{ReviewMe, "every required check passed"}
	default: // pending
		if waited(in) {
			return Result{NeedsYou, "no matching rollup within the wait"}
		}
		return Result{Checking, "waiting on checks"}
	}
}

// waited reports whether the bounded wait has elapsed. in.Now is never wall clock in practice —
// the caller derives it from ticks whose observe phase succeeded, which is what keeps an outage
// from walking every row to needs_you at once (docs/command-centre-design.md § 11 inv. 11).
func waited(in Input) bool {
	return !in.PushedAt.IsZero() && in.Now.Sub(in.PushedAt) >= BoundedWait
}

// resolve walks one predicate node. A node is exactly one of a combinator (AllOf/AnyOf/Not) or
// a leaf; encoding/toml only ever produces that shape from [repo.checks].
func resolve(p Predicate, in Input) triState {
	switch {
	case len(p.AllOf) > 0:
		return allOf(p.AllOf, in)
	case len(p.AnyOf) > 0:
		return anyOf(p.AnyOf, in)
	case p.Not != nil:
		return not(resolve(*p.Not, in))
	default:
		return leaf(p, in)
	}
}

// allOf is red the moment any child is red (no point waiting out the rest), pending while
// nothing is red but something still is, and green only once every child is.
func allOf(ps []Predicate, in Input) triState {
	result := green
	for _, p := range ps {
		switch resolve(p, in) {
		case red:
			return red
		case pending:
			result = pending
		case green:
			// no-op: result only ever downgrades from its green start
		}
	}
	return result
}

// anyOf is green the moment any child is, red only once every child is, and pending otherwise —
// a still-pending arm could yet turn the whole thing green.
func anyOf(ps []Predicate, in Input) triState {
	result := red
	for _, p := range ps {
		switch resolve(p, in) {
		case green:
			return green
		case pending:
			result = pending
		case red:
			// no-op: result only ever upgrades from its red start
		}
	}
	return result
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

// leaf resolves one non-combinator node: a named check's required conclusion, or the author
// escape hatch. Exactly one of these fields is set by construction (see Predicate's doc).
func leaf(p Predicate, in Input) triState {
	switch {
	case p.Success != "":
		return requireConclusion(in.Checks[p.Success], Success)
	case p.Skipped != "":
		return requireConclusion(in.Checks[p.Skipped], Skipped)
	case p.AbsentOK != "":
		return absentOK(in.Checks, p.AbsentOK, in)
	case p.Author != "":
		return author(in.AuthorLogin, p.Author)
	default:
		return pending // an empty leaf (misconfigured [repo.checks]) asks forever, never lies green
	}
}

// requireConclusion resolves a plain success/skipped leaf: green once the check reports exactly
// the required conclusion, red once it reports a different final one (there is no retry-pending
// rule -- red is red, docs/command-centre-design.md § 11), pending otherwise, including a check
// absent from the rollup (a missing map key already reads Pending).
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

// absentOK resolves the leniency the path-filtered "Lint GitHub Actions / Lint" check needs:
// approximating Mergify's own "-check-neutral AND -check-pending AND -check-failure" (docs/
// command-centre-design.md § 8), a check absent from the rollup reads pending until the bounded
// wait elapses, then passing -- never green from a young snapshot, which would reopen per-check
// the hole the rollup-level rule closes. A check that did run must still conclude success or
// skipped; a completed failure is red regardless of the wait.
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

// author resolves the dependabot arm: a PR attribute, not a check-run, which is why the grammar
// needed a leaf outside the success/skipped/absent_ok check-name family. It is never pending —
// the author is known the moment the PR is.
func author(got, want string) triState {
	if got == want {
		return green
	}
	return red
}
