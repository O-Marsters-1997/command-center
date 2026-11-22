package demo

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

const (
	maxDepth     = 3
	mainHorizon  = 45 * time.Minute
	compatCheck  = "compat"
	cleanOutcome = 0.65
)

var (
	repoNames    = []string{"acme/api", "acme/web", "acme/worker"}
	featureNames = []string{"health", "billing", "search"}
	verbs        = []string{"Add", "Wire", "Extract", "Cache", "Document", "Retry"}
	nouns        = []string{
		"config loader", "metrics endpoint", "audit log", "session store", "export job",
		"rate limiter", "health probe", "feature flags", "webhook sender", "search index",
	}
)

// Generate produces a reproducible scenario for a seed and ticket count.
func Generate(seed int64, tickets int) Scenario {
	//nolint:gosec // reproducible demo data, not security
	rng := rand.New(rand.NewPCG(uint64(seed), 0))

	repos := make([]Repo, 2+rng.IntN(2))
	for n := range repos {
		repos[n] = Repo{
			Name:     repoNames[n],
			Stacking: n == 0,
			Files:    seedFiles(repoNames[n]),
		}
	}
	repos[1].CompatCheck = compatCheck
	features := featureNames[:2+rng.IntN(2)]

	sc := Scenario{Seed: seed, Repos: repos}
	depth := make([]int, 0, tickets)
	for n := range tickets {
		feature := rng.IntN(len(features))
		t := Ticket{
			ID:      fmt.Sprintf("T%d", n+1),
			Repo:    repos[feature%len(repos)].Name,
			Title:   verbs[rng.IntN(len(verbs))] + " the " + nouns[n%len(nouns)],
			Feature: features[feature],
		}
		var level int
		t.BlockedBy, level = pickBlockers(rng, sc.Tickets, depth, t)
		depth = append(depth, level)
		drawScript(rng, &t, repos[feature%len(repos)])
		if n == 0 {
			multiFolderScript(&t)
		}
		sc.Tickets = append(sc.Tickets, t)
	}
	sc.Main = mainCommits(rng, repos)
	return sc
}

func pickBlockers(rng *rand.Rand, earlier []Ticket, depth []int, t Ticket) ([]string, int) {
	var candidates []int
	for n, e := range earlier {
		if e.Feature == t.Feature && depth[n] < maxDepth {
			candidates = append(candidates, n)
		}
	}
	rng.Shuffle(len(candidates), func(a, b int) { candidates[a], candidates[b] = candidates[b], candidates[a] })
	candidates = candidates[:min(rng.IntN(3), len(candidates))]
	slices.Sort(candidates)

	level := 1
	blockers := make([]string, 0, len(candidates))
	for _, n := range candidates {
		blockers = append(blockers, earlier[n].ID)
		level = max(level, depth[n]+1)
	}
	return blockers, level
}

func drawScript(rng *rand.Rand, t *Ticket, repo Repo) {
	t.AgentAfter = between(rng, 20*time.Second, 90*time.Second)
	t.CIAfter = between(rng, 10*time.Second, 40*time.Second)
	t.MergeAfter = between(rng, 30*time.Second, 120*time.Second)
	t.Result = resultCommits
	t.CI = []string{ciPass}
	t.Files = map[string]string{strings.ToLower(t.ID) + ".go": "package main\n"}

	if rng.Float64() < cleanOutcome {
		return
	}
	switch rng.IntN(5) {
	case 0:
		t.Result = resultCrash
	case 1:
		t.CI = []string{ciFail, ciPass}
	case 2:
		t.Result = resultConflict
	case 3:
		if repo.CompatCheck != "" {
			t.CompatFails = true
		} else {
			t.CI = []string{ciFail, ciPass}
		}
	default:
		t.AgentAfter = between(rng, 4*time.Minute, 6*time.Minute)
	}
}

var editedFolders = []string{"api", "store", "queue", "worker", "cache", "metrics", "auth"}

const legacyFile = "internal/legacy/legacy.go"

func seedFiles(repo string) map[string]string {
	files := map[string]string{
		"go.mod":      "module " + repo + "\n",
		"main.go":     "package main\n",
		legacyFile:    funcBody("legacy"),
		"docs/api.md": "# API\n",
	}
	for _, f := range editedFolders {
		files["internal/"+f+"/"+f+".go"] = funcBody(f)
	}
	return files
}

func multiFolderScript(t *Ticket) {
	t.Result = resultCommits
	t.CI = []string{ciPass}
	t.CompatFails = false
	t.Files = map[string]string{
		legacyFile:           "",
		"docs/api.md":        "# API\n\nThe handlers now return typed errors.\n",
		"docs/changes.md":    "# Changes\n\n- typed errors\n",
		"internal/api/v2.go": "package api\n\nfunc routesV2() {}\n",
	}
	for _, f := range editedFolders {
		t.Files["internal/"+f+"/"+f+".go"] = funcBody(f, 4, 34)
	}
}

func funcBody(pkg string, tuned ...int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nfunc run() {\n", pkg)
	for n := 1; n <= 36; n++ {
		suffix := ""
		if slices.Contains(tuned, n) {
			suffix = " // tuned"
		}
		fmt.Fprintf(&b, "\tstep%02d()%s\n", n, suffix)
	}
	b.WriteString("}\n")
	return b.String()
}

func mainCommits(rng *rand.Rand, repos []Repo) []Main {
	var out []Main
	for _, repo := range repos {
		for n, at := 1, between(rng, 2*time.Minute, 4*time.Minute); at < mainHorizon; n++ {
			out = append(out, Main{At: at, Repo: repo.Name, Files: map[string]string{
				fmt.Sprintf("main-%d.go", n): "package main\n",
			}})
			at += between(rng, 2*time.Minute, 4*time.Minute)
		}
	}
	slices.SortStableFunc(out, func(a, b Main) int { return cmp.Compare(a.At, b.At) })
	return out
}

func between(rng *rand.Rand, lo, hi time.Duration) time.Duration {
	return lo + time.Duration(rng.Int64N(int64((hi-lo)/time.Second)+1))*time.Second
}
