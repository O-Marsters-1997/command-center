package web_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func activeLaunchHashes(t *testing.T, store *storepkg.Store) map[string]string {
	t.Helper()
	if err := store.ApplyLaunchIntents(t.Context(), testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	hashes, err := store.ActiveLaunchHashes(t.Context())
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	return hashes
}

func composedHash(ticketURL string) string {
	return plan.Hash(plan.Compose(plan.Ticket{URL: plan.TicketURL(ticketURL)}))
}

func TestLaunch(t *testing.T) {
	t.Parallel()

	t.Run("a same-origin post queues the ticket with its composed hash", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t, storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"})

		got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/launch?ticket=sandbox://CC-1", req{})
		if got.Code != http.StatusSeeOther || got.Location != "/" {
			t.Fatalf("POST /launch = %d %q, want 303 /", got.Code, got.Location)
		}
		want := map[string]string{"sandbox://CC-1": composedHash("sandbox://CC-1")}
		if diff := cmp.Diff(want, activeLaunchHashes(t, store)); diff != "" {
			t.Errorf("active launch hashes mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("accepts repeated form-encoded tickets", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)

		response := do(t, newServer(store, testNow), http.MethodPost, "/launch", req{
			Form: url.Values{"ticket": {"sandbox://CC-1", "sandbox://CC-2"}},
		})
		if response.Code != http.StatusSeeOther || response.Location != "/" {
			t.Fatalf("POST /launch = %d %q, want 303 /", response.Code, response.Location)
		}
		hashes := activeLaunchHashes(t, store)
		if _, ok := hashes["sandbox://CC-1"]; !ok || len(hashes) != 2 {
			t.Errorf("active launch hashes = %v, want CC-1 and CC-2", hashes)
		}
	})

	t.Run("handles an arbitrarily sized slice", func(t *testing.T) {
		t.Parallel()
		const fanOut = 50
		root := storepkg.Ticket{URL: "sandbox://CC-0", Repo: "cc-sandbox", Branch: "cc-0"}
		tickets := []storepkg.Ticket{root}
		query := "ticket=" + root.URL
		for i := 1; i <= fanOut; i++ {
			ticketURL := fmt.Sprintf("sandbox://CC-%d", i)
			tickets = append(tickets, storepkg.Ticket{
				URL: ticketURL, Repo: "cc-sandbox", Branch: fmt.Sprintf("cc-%d", i), BlockedBy: []string{root.URL},
			})
			query += "&ticket=" + ticketURL
		}
		store := candidateStore(t, tickets...)
		server := openServer(store, fixedClock(testNow), "")

		candidates := do(t, server, http.MethodGet, "/launch/candidates.json?"+query, req{})
		var decoded []jsonCandidate
		if err := json.Unmarshal([]byte(candidates.Body), &decoded); err != nil {
			t.Fatalf("decode /launch/candidates.json: %v", err)
		}
		if len(decoded) != fanOut+1 {
			t.Errorf("GET /launch/candidates.json returned %d candidates, want %d", len(decoded), fanOut+1)
		}
		if got := do(t, server, http.MethodPost, "/launch?"+query, req{}); got.Code != http.StatusSeeOther {
			t.Fatalf("POST /launch status = %d, want 303", got.Code)
		}
		if got := len(activeLaunchHashes(t, store)); got != fanOut+1 {
			t.Errorf("POST /launch authorised %d tickets, want %d", got, fanOut+1)
		}
	})

	t.Run("refuses a submitted hash that no longer composes", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
		)
		previewed := plan.Hash("a prompt sandbox://CC-2 no longer composes to")
		form := url.Values{
			"ticket": {"sandbox://CC-1", "sandbox://CC-2"},
			"hash":   {"sandbox://CC-1 " + composedHash("sandbox://CC-1"), "sandbox://CC-2 " + previewed},
		}

		got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/launch", req{Form: form})
		if got.Code != http.StatusConflict {
			t.Fatalf("POST /launch status = %d, want 409", got.Code)
		}
		for _, want := range []string{"sandbox://CC-2", previewed, composedHash("sandbox://CC-2")} {
			if !strings.Contains(got.Body, want) {
				t.Errorf("POST /launch 409 body does not name %q", want)
			}
		}
		if hashes := activeLaunchHashes(t, store); len(hashes) != 0 {
			t.Errorf("active launch hashes = %v, want none", hashes)
		}
	})

	t.Run("ignores the hash of an unchecked row", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		form := url.Values{
			"ticket": {"sandbox://CC-1"},
			"hash":   {"sandbox://CC-1 " + composedHash("sandbox://CC-1"), "sandbox://CC-2 " + composedHash("sandbox://CC-2")},
		}

		got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/launch", req{Form: form})
		if got.Code != http.StatusSeeOther || got.Location != "/" {
			t.Fatalf("POST /launch = %d %q, want 303 /", got.Code, got.Location)
		}
		want := map[string]string{"sandbox://CC-1": composedHash("sandbox://CC-1")}
		if diff := cmp.Diff(want, activeLaunchHashes(t, store)); diff != "" {
			t.Errorf("active launch hashes mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("hashes the launch context and keeps the previewed hash valid", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t, storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"})
		form := url.Values{
			"ticket":  {"sandbox://CC-1"},
			"hash":    {"sandbox://CC-1 " + composedHash("sandbox://CC-1")},
			"context": {"  prefer table tests  "},
			"model":   {"opus"},
			"effort":  {"high"},
		}

		got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/launch", req{Form: form})
		if got.Code != http.StatusSeeOther {
			t.Fatalf("POST /launch = %d, want 303", got.Code)
		}
		withContext := plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-1", LaunchContext: "prefer table tests"}))
		want := map[string]string{"sandbox://CC-1": withContext}
		if diff := cmp.Diff(want, activeLaunchHashes(t, store)); diff != "" {
			t.Errorf("active launch hashes mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("rejects a model, effort or workflow outside the offered set", func(t *testing.T) {
		t.Parallel()
		for _, field := range []url.Values{{"model": {"gpt-9"}}, {"effort": {"extreme"}}, {"workflow": {"v9"}}} {
			field.Set("ticket", "sandbox://CC-1")
			server := openServer(seededStore(t, testNow), fixedClock(testNow), "")
			if got := do(t, server, http.MethodPost, "/launch", req{Form: field}); got.Code != http.StatusBadRequest {
				t.Errorf("POST /launch %v status = %d, want 400", field, got.Code)
			}
		}
	})

	t.Run("refuses a workflow other than v0 when agent_command has no {tools}", func(t *testing.T) {
		t.Parallel()
		deps := web.Deps{
			Logger: slog.New(slog.DiscardHandler), Store: seededStore(t, testNow), Clock: fixedClock(testNow),
			AgentCommand: []string{"claude", "-p", "{prompt}"},
		}
		server := web.NewServer(deps)
		for workflow, want := range map[string]int{"v1": http.StatusBadRequest, "v0": http.StatusSeeOther} {
			form := url.Values{"ticket": {"sandbox://CC-1"}, "workflow": {workflow}}
			if got := do(t, server, http.MethodPost, "/launch", req{Form: form}); got.Code != want {
				t.Errorf("POST /launch workflow=%s status = %d, want %d", workflow, got.Code, want)
			}
		}
	})

	t.Run("rejects a malformed hash field", func(t *testing.T) {
		t.Parallel()
		form := url.Values{"ticket": {"sandbox://CC-1"}, "hash": {"deadbeef"}}
		server := openServer(seededStore(t, testNow), fixedClock(testNow), "")
		got := do(t, server, http.MethodPost, "/launch", req{Form: form})
		if got.Code != http.StatusBadRequest {
			t.Errorf("POST /launch hash=deadbeef status = %d, want 400", got.Code)
		}
	})
}
