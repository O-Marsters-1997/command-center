package config

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const DefaultWorkflow = "v0"

//go:embed workflows/v1/*.md
var v1Prompts embed.FS

func v1Prompt(step string) string {
	b, err := v1Prompts.ReadFile("workflows/v1/" + step + ".md")
	if err != nil {
		panic("read v1 prompt " + step + ": " + err.Error())
	}
	return string(b)
}

func v1() plan.Workflow {
	return plan.Workflow{
		Name:         "v1",
		MaxFixRounds: 2,
		Steps: []plan.Step{
			{Name: "setup", Command: true},
			{Name: "read", Model: "haiku", Tools: []string{"Read", "Grep", "Glob"}, Writes: true, Prompt: v1Prompt("read")},
			{
				Name: "implement", Model: "sonnet", Tools: []string{"Read", "Edit", "Write", "Bash"},
				Prompt: v1Prompt("implement"),
			},
			{Name: "check", Command: true},
			{
				Name: "review", Model: "sonnet", Tools: []string{"Read", "Grep", "Glob"}, Writes: true,
				Prompt: v1Prompt("review"),
			},
			{
				Name: "fix", Model: "sonnet", Tools: []string{"Read", "Edit", "Bash"}, OnFail: true,
				Prompt: v1Prompt("fix"),
			},
			{Name: "pr", Model: "haiku", Tools: []string{"Read"}, Writes: true, Prompt: v1Prompt("pr")},
		},
	}
}

// Workflows returns every built-in workflow, each with its hash set.
func Workflows() map[string]plan.Workflow {
	builtin := []plan.Workflow{
		{Name: DefaultWorkflow, Steps: []plan.Step{{Name: "implement"}}},
		v1(),
	}
	out := make(map[string]plan.Workflow, len(builtin))
	for _, w := range builtin {
		w.Hash = hashWorkflow(w)
		out[w.Name] = w
	}
	return out
}

func hashWorkflow(w plan.Workflow) string {
	w.Hash = ""
	b, err := json.Marshal(w)
	if err != nil {
		panic("marshal workflow " + w.Name + ": " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
