package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const DefaultWorkflow = "v0"

// Workflows returns every built-in workflow, each with its hash set.
func Workflows() map[string]plan.Workflow {
	builtin := []plan.Workflow{
		{Name: DefaultWorkflow, Steps: []plan.Step{{Name: "implement"}}},
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
