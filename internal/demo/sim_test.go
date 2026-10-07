//go:build demo

package demo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var recoveryVerb = map[string]string{
	"failed":              "re-run",
	"ci_failed":           "re-run",
	"conflicts_with_main": "resolve",
	"conflict_resolved":   "commit-resolution",
}

func TestGeneratedBoardDrainsAfterRecovery(t *testing.T) {
	sim, err := NewSim(t.Context(), Generate(6, 12))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sim.Close(); err != nil {
			t.Error(err)
		}
	})

	seen := map[string]bool{}
	pressed := map[string]string{}
	for tick := 0; tick < 300 && !allMerged(sim); tick++ {
		if err := sim.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		for _, i := range sim.issues {
			state := sim.board[i.ID]
			seen[state] = true
			verb, ok := recoveryVerb[state]
			if !ok || pressed[i.ID] == state {
				continue
			}
			pressed[i.ID] = state
			press(t, sim, verb, i.url)
		}
	}

	for _, want := range []string{"failed", "ci_failed", "conflicts_with_main"} {
		if !seen[want] {
			t.Errorf("no ticket ever reached %s", want)
		}
	}
	for _, i := range sim.issues {
		if got := sim.board[i.ID]; got != "merged" {
			t.Errorf("ticket %s ended %q, want merged", i.ID, got)
		}
	}
}

func allMerged(sim *Sim) bool {
	for _, i := range sim.issues {
		if sim.board[i.ID] != "merged" {
			return false
		}
	}
	return true
}

func press(t *testing.T, sim *Sim, verb, ticket string) {
	t.Helper()
	form := url.Values{"verb": {verb}, "ticket": {ticket}}
	req := httptest.NewRequest(http.MethodPost, "/verb", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	sim.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /verb %s %s = %d: %s", verb, ticket, rec.Code, rec.Body)
	}
}
