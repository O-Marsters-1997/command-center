// Package loop is the Command Centre's imperative shell: the tick loop and its steps.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package loop

import "github.com/O-Marsters-1997/command-center/internal/config"

func verifyCommandByRepo(repos []config.Repo) map[string][]string {
	m := make(map[string][]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.VerifyCommand
	}
	return m
}

func generatedByRepo(repos []config.Repo) map[string][]string {
	m := make(map[string][]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Generated
	}
	return m
}

func buildCommandByRepo(repos []config.Repo) map[string][]string {
	m := make(map[string][]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.BuildCommand
	}
	return m
}

func repoPathsByName(repos []config.Repo) map[string]string {
	m := make(map[string]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Checkout
	}
	return m
}
