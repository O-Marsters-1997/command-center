package web_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenStore(cctest.DSN(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time                       { return c.at }
func (frozenClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func fixedClock(at time.Time) web.Clock { return frozenClock{at} }

// dispositionAsPushed records a run whose disposition is already known to be push, so a test can
// read what the board shows for a pushed ticket without driving the loop through spawn and dispose.
func dispositionAsPushed(t *testing.T, st *store.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := st.InsertRunSkeleton(t.Context(), ticketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSpawn(t.Context(), runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := st.RecordDisposition(t.Context(), runID, plan.OutcomePush, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
}

// oneMillionInputTokensLine is one $6.40 (at the calibrated sonnet rate) assistant request, for a
// test to place at a chosen timestamp and request id.
func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
