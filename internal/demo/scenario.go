package demo

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration is a scenario time written in real-world units, such as "12m" or "2h".
type Duration time.Duration

// UnmarshalText parses a time.ParseDuration string.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("duration %q: %w", text, err)
	}
	*d = Duration(parsed)
	return nil
}

// Scenario is one demo run read from a TOML file: the repos, the ticket DAG, each ticket's step
// script, and the checkpoints the run is checked against. Every Duration is sim time since the
// run started, except Merge.After and Close.After, which count from the moment the ticket's PR
// opens, and CIAfter, which counts from the push or re-run that started the check.
type Scenario struct {
	Seed   int64    `toml:"seed"`
	Speed  float64  `toml:"speed"`
	Repos  []Repo   `toml:"repo"`
	Ticket []Ticket `toml:"ticket"`
	Main   []Main   `toml:"main"`
	Push   []Push   `toml:"push"`
	Press  []Press  `toml:"press"`
	Expect []Expect `toml:"expect"`
}

// Repo is one repository the sandbox hosts a bare origin for, seeded with Files on main.
type Repo struct {
	Name     string            `toml:"name"`
	Stacking bool              `toml:"stacking"`
	Files    map[string]string `toml:"files"`
}

// Ticket is one issue in the DAG and the script the world plays for it.
type Ticket struct {
	ID        string      `toml:"id"`
	Repo      string      `toml:"repo"`
	Title     string      `toml:"title"`
	Feature   string      `toml:"feature"`
	BlockedBy []string    `toml:"blocked_by"`
	Agent     AgentScript `toml:"agent"`
	CI        []string    `toml:"ci"`
	CIAfter   Duration    `toml:"ci_after"`
	Merge     Step        `toml:"merge"`
	Close     Step        `toml:"close"`
	Cut       string      `toml:"cut"`
	Push      string      `toml:"push"`
	Verify    string      `toml:"verify"`
}

// AgentScript is what the fake agent does once spawned for a ticket.
type AgentScript struct {
	After  Duration `toml:"after"`
	Result string   `toml:"result"`
	Files  []string `toml:"files"`
}

// Step is a scripted event that happens After a point the field's owner defines.
type Step struct {
	After Duration `toml:"after"`
}

// Main is a commit landing on a repo's origin main at sim time At, not from cc.
type Main struct {
	At    Duration          `toml:"at"`
	Repo  string            `toml:"repo"`
	Files map[string]string `toml:"files"`
}

// Push is a commit a human pushes to Ticket's branch on origin at sim time At.
type Push struct {
	At     Duration          `toml:"at"`
	Ticket string            `toml:"ticket"`
	Files  map[string]string `toml:"files"`
}

// Press is a verb a human presses on Ticket's row at sim time At.
type Press struct {
	At     Duration `toml:"at"`
	Ticket string   `toml:"ticket"`
	Verb   string   `toml:"verb"`
}

// Expect is a checkpoint: at sim time At, Ticket must be in State.
type Expect struct {
	At     Duration `toml:"at"`
	Ticket string   `toml:"ticket"`
	State  string   `toml:"state"`
}

// LoadScenario reads and validates the scenario at path.
func LoadScenario(path string) (Scenario, error) {
	var s Scenario
	if _, err := toml.DecodeFile(path, &s); err != nil {
		return Scenario{}, fmt.Errorf("read scenario %s: %w", path, err)
	}
	slices.SortStableFunc(s.Expect, func(a, b Expect) int { return cmp.Compare(a.At, b.At) })
	slices.SortStableFunc(s.Main, func(a, b Main) int { return cmp.Compare(a.At, b.At) })
	slices.SortStableFunc(s.Push, func(a, b Push) int { return cmp.Compare(a.At, b.At) })
	slices.SortStableFunc(s.Press, func(a, b Press) int { return cmp.Compare(a.At, b.At) })
	if err := s.validate(); err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", path, err)
	}
	return s, nil
}

func (s Scenario) validate() error {
	repos := map[string]bool{}
	for _, r := range s.Repos {
		repos[r.Name] = true
	}
	ids := map[string]bool{}
	for _, t := range s.Ticket {
		if ids[t.ID] {
			return fmt.Errorf("ticket %q declared twice", t.ID)
		}
		ids[t.ID] = true
		if !repos[t.Repo] {
			return fmt.Errorf("ticket %q names unknown repo %q", t.ID, t.Repo)
		}
		if !slices.Contains([]string{"commits", "conflict", "crash"}, t.Agent.Result) {
			return fmt.Errorf("ticket %q: agent result %q is not supported yet", t.ID, t.Agent.Result)
		}
		scripted := []struct{ name, value string }{{"cut", t.Cut}, {"push", t.Push}, {"verify", t.Verify}}
		for _, f := range scripted {
			if f.value != "" && f.value != "fail" {
				return fmt.Errorf("ticket %q: %s %q is not supported, want \"fail\"", t.ID, f.name, f.value)
			}
		}
		for _, ci := range t.CI {
			if !slices.Contains([]string{ciPass, ciFail, ciHang}, ci) {
				return fmt.Errorf("ticket %q: ci %q is not pass, fail or hang", t.ID, ci)
			}
		}
	}
	for _, t := range s.Ticket {
		for _, blocker := range t.BlockedBy {
			if !ids[blocker] {
				return fmt.Errorf("ticket %q is blocked by unknown ticket %q", t.ID, blocker)
			}
		}
	}
	for _, m := range s.Main {
		if !repos[m.Repo] {
			return fmt.Errorf("main event names unknown repo %q", m.Repo)
		}
		if len(m.Files) == 0 {
			return errors.New("main event commits no files")
		}
	}
	for _, p := range s.Push {
		if !ids[p.Ticket] {
			return fmt.Errorf("push names unknown ticket %q", p.Ticket)
		}
		if len(p.Files) == 0 {
			return fmt.Errorf("push to ticket %q commits no files", p.Ticket)
		}
	}
	for _, p := range s.Press {
		if !ids[p.Ticket] {
			return fmt.Errorf("press names unknown ticket %q", p.Ticket)
		}
		if p.Verb == "" {
			return fmt.Errorf("press on ticket %q names no verb", p.Ticket)
		}
	}
	for _, e := range s.Expect {
		if !ids[e.Ticket] {
			return fmt.Errorf("expect names unknown ticket %q", e.Ticket)
		}
	}
	return nil
}
