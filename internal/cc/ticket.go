// Package cc is the Command Centre's imperative shell: store, loop and page.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package cc

import "github.com/O-Marsters-1997/command-center/internal/config"

// Ticket is one tracked issue. Source, Title, Body, Status, Feature and SyncedAt are the
// tracker's own, refreshed on every import; Repo is matched from URL against a [[repo]]'s remote.
// Branch and BlockedBy are the app's own, seeded once on a URL's first import, then left alone.
// FirstPushCI and HandChurnLines are nil until recordFirstPushCI/recordMergedEvents observe the
// fact they report, and never overwritten after that.
type Ticket struct {
	URL            string
	Repo           string
	Branch         string
	BlockedBy      []string
	Source         string
	Title          string
	Body           string
	Status         string
	Feature        string
	SyncedAt       string
	FirstPushCI    *bool
	HandChurnLines *int
}

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
