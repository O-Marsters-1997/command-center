package cc_test

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

var update = flag.Bool("update", false, "regenerate golden files")

const goldenPage = "testdata/page.golden.html"

func seededStore(t *testing.T, observedAt time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		ObservedAt: observedAt,
		PRs:        map[string]gh.PR{},
		Worktrees:  map[string]string{"cc-1-first": "/repos/cc-sandbox-cc-1-first"},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	tickErr := cc.TickError{At: observedAt.Add(15 * time.Second), Message: "observe: gh pr list: exit status 1"}
	if err := store.RecordTickError(ctx, tickErr); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestServerRendersThePage(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := observedAt.Add(45 * time.Second)
	server := cc.NewServer(seededStore(t, observedAt), fixedClock(now), nil)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	got := rec.Body.Bytes()
	if *update {
		if err := os.WriteFile(goldenPage, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPage)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("page differs from %s; rerun with -update to accept\n--- got ---\n%s", goldenPage, got)
	}
}

func TestServerRejectsUnknownPaths(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLaunchRejectsBadOriginAndMethod(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
	}{
		{
			name:   "GET is rejected before origin is even checked",
			method: http.MethodGet, origin: srv.URL, wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "a missing Origin is rejected",
			method: http.MethodPost, origin: "", wantStatus: http.StatusForbidden,
		},
		{
			name:   "a foreign Origin is rejected",
			method: http.MethodPost, origin: "http://evil.example", wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+"/launch?task=sandbox://CC-1", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestLaunchAcceptsASameOriginPost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?task=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202: %s", resp.StatusCode, body)
	}
}

func TestPreviewRendersNowOnUnlockAndRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"}},
		{TicketURL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil))
	t.Cleanup(srv.Close)

	// CC-4 is a tracked task but deliberately left out of the slice, so CC-3's blocker sits
	// outside it with no pull request.
	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CC-1&task=sandbox://CC-2&task=sandbox://CC-3")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode preview response %s: %v", body, err)
	}
	byTicket := make(map[string]map[string]any, len(rows))
	for _, r := range rows {
		byTicket[r["TicketURL"].(string)] = r
	}

	if got := byTicket["sandbox://CC-1"]["Label"]; got != "now" {
		t.Errorf("CC-1 label = %v, want now", got)
	}
	if got := byTicket["sandbox://CC-1"]["Base"]; got != "origin/main" {
		t.Errorf("CC-1 base = %v, want origin/main", got)
	}
	if got := byTicket["sandbox://CC-2"]["Label"]; got != "on unlock" {
		t.Errorf("CC-2 label = %v, want on unlock", got)
	}
	if got := byTicket["sandbox://CC-2"]["Base"]; got != "origin/main" {
		t.Errorf("CC-2 base = %v, want origin/main", got)
	}
	if got := byTicket["sandbox://CC-3"]["Label"]; got != "refused" {
		t.Errorf("CC-3 label = %v, want refused", got)
	}
	if reason, _ := byTicket["sandbox://CC-3"]["Reason"].(string); !strings.Contains(reason, "sandbox://CC-4") {
		t.Errorf("CC-3 reason = %q, does not name its blocker sandbox://CC-4", reason)
	}
}

func TestPreviewRejectsEmptyOrUnknownTask(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		path string
	}{
		{name: "no task at all", path: "/preview"},
		{name: "an empty task value", path: "/preview?task="},
		{name: "an unknown ticket", path: "/preview?task=sandbox://GHOST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}
