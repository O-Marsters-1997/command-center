package agentlog

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var ErrNoParseableLines = errors.New("no parseable lines")

type RunMetrics struct {
	CostUSD    option.Option[float64]       `json:"-"`
	Requests   []RequestUsage               `json:"-"`
	Wall       option.Option[time.Duration] `json:"wall"`
	Turns      int                          `json:"turns"`
	Input      int64                        `json:"input"`
	CacheWrite int64                        `json:"cache_write"`
	CacheRead  int64                        `json:"cache_read"`
	Output     int64                        `json:"output"`
	Tools      map[string]int               `json:"tools"`
	Skills     []string                     `json:"skills,omitempty"`
	Phases     []PhaseTokens                `json:"phases,omitempty"`
	InFlight   []string                     `json:"in_flight,omitempty"`
}

type taskLedger struct {
	labels   map[string]string
	order    []string
	finished map[string]bool
}

func (t *taskLedger) observe(parsed logLine) {
	if parsed.Type != "system" || parsed.TaskID == "" {
		return
	}
	if t.labels == nil {
		t.labels, t.finished = map[string]string{}, map[string]bool{}
	}
	switch parsed.Subtype {
	case "task_started":
		t.labels[parsed.TaskID] = taskLabel(parsed)
		t.order = append(t.order, parsed.TaskID)
	case "task_notification":
		t.finished[parsed.TaskID] = parsed.Status != "stopped"
	}
}

func taskLabel(parsed logLine) string {
	switch {
	case parsed.SubagentType != "":
		return parsed.SubagentType + " subagent"
	case parsed.TaskType == "local_bash":
		return "background command"
	}
	return "background task"
}

func (t *taskLedger) inFlight() []string {
	var labels []string
	for _, id := range t.order {
		if label := t.labels[id]; !t.finished[id] && !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
	}
	return labels
}

type PhaseTokens struct {
	Skill     string  `json:"skill"`
	Tokens    int64   `json:"tokens"`
	WeightUSD float64 `json:"weight"`
}

func ParseLastMessage(logPath string) (option.Option[string], error) {
	last := option.None[string]()
	err := forEachLine(logPath, func(parsed logLine, _ time.Time) {
		if parsed.Type == "result" && parsed.ResultText != "" {
			last = option.Some(parsed.ResultText)
		}
	})
	if err != nil {
		return option.None[string](), fmt.Errorf("parse last message: %w", err)
	}
	return last, nil
}

// ParseMetrics reads Claude stream-json usage, rejecting logs with no parseable lines.
func ParseMetrics(logPath string) (RunMetrics, error) {
	var metrics RunMetrics
	skillOf := map[string]string{}
	var requestSkills []string
	tasks := taskLedger{}
	requests, settled, decoded, err := readUsage(logPath, func(parsed logLine, added bool) {
		tasks.observe(parsed)
		if parsed.Type == "result" {
			metrics.Wall = option.Some(time.Duration(parsed.DurationMS) * time.Millisecond)
			return
		}
		if added {
			skill, ok := skillOf[parsed.ParentToolUseID]
			if !ok {
				skill = skillOf[""]
			}
			requestSkills = append(requestSkills, skill)
			if parsed.ParentToolUseID == "" {
				metrics.Turns++
			}
		}
		if parsed.Type != "assistant" {
			return
		}
		for _, block := range parsed.Message.Content {
			if block.Type != "tool_use" {
				continue
			}
			if event := block.toolEvent(); event.Kind == Skill {
				skillOf[parsed.ParentToolUseID] = event.Tool
				if !slices.Contains(metrics.Skills, event.Tool) {
					metrics.Skills = append(metrics.Skills, event.Tool)
				}
			}
			if metrics.Tools == nil {
				metrics.Tools = make(map[string]int)
			}
			metrics.Tools[block.Name]++
		}
	})
	if err != nil {
		return RunMetrics{}, fmt.Errorf("parse run metrics: %w", err)
	}
	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: %w", logPath, ErrNoParseableLines)
	}
	metrics.Requests = requests
	metrics.InFlight = tasks.inFlight()
	index := map[string]int{}
	for i, r := range requests {
		j, ok := index[requestSkills[i]]
		if !ok {
			j = len(metrics.Phases)
			index[requestSkills[i]] = j
			metrics.Phases = append(metrics.Phases, PhaseTokens{Skill: requestSkills[i]})
		}
		metrics.Phases[j].Tokens += r.Input + r.CacheCreate + r.Output
		metrics.Phases[j].WeightUSD += Weight(r)
	}
	totals := requests
	if settled.IsSome() {
		metrics.CostUSD = settled.Unwrap().CostUSD
		totals = []RequestUsage{settled.Unwrap()}
	}
	for _, r := range totals {
		metrics.Input += r.Input
		metrics.CacheWrite += r.CacheCreate
		metrics.CacheRead += r.CacheRead
		metrics.Output += r.Output
	}
	return metrics, nil
}
