package cc_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestRunningRowsPillPulsesUnattendedDisc(t *testing.T) {
	t.Parallel()

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)

	server := cc.NewServer(store, fixedClock(now), nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `class="pill pill-live pill-disc pill-pulse"`) {
		t.Errorf("running row's pill is not a pulsing, unattended, live pill:\n%s", body)
	}
}

func TestEndedRunsPillDoesNotPulse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	if strings.Contains(body, "pill-pulse") {
		t.Errorf("no run is alive, but a pill still pulses:\n%s", body)
	}
	if !strings.Contains(body, `class="pill pill-idle pill-ring"`) {
		t.Errorf("ready row is not an idle ring:\n%s", body)
	}
	if !strings.Contains(body, `class="pill pill-wait pill-ring"`) {
		t.Errorf("blocked row is not a wait ring:\n%s", body)
	}
}
