package view

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestCmdLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		event      agentlog.Event
		wantResult string
		wantFailed bool
		wantFold   bool
		wantOpen   bool
	}{
		{
			name:       "a pass shows a tick and its duration, output folded",
			event:      agentlog.Event{Kind: agentlog.Cmd, Done: true, Elapsed: 1200 * time.Millisecond, Output: "ok"},
			wantResult: "✓ 1.2s",
			wantFold:   true,
		},
		{
			name:       "a failure shows its exit code, output open",
			event:      agentlog.Event{Kind: agentlog.Cmd, Done: true, ExitCode: 2, Elapsed: 3 * time.Second, Output: "boom"},
			wantResult: "exit 2 3s",
			wantFailed: true,
			wantFold:   true,
			wantOpen:   true,
		},
		{
			name:       "interrupted is not a pass",
			event:      agentlog.Event{Kind: agentlog.Cmd, Done: true, Interrupted: true},
			wantResult: "interrupted",
			wantFailed: true,
		},
		{
			name:       "no result yet is running",
			event:      agentlog.Event{Kind: agentlog.Cmd},
			wantResult: "running",
		},
		{
			name:       "a result with no duration shows none",
			event:      agentlog.Event{Kind: agentlog.Cmd, Done: true},
			wantResult: "✓",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := LineOf(tt.event)
			if line.Result != tt.wantResult || line.Failed != tt.wantFailed ||
				line.Fold != tt.wantFold || line.Open != tt.wantOpen {
				t.Errorf("LineOf(%+v) = {Result:%q Failed:%v Fold:%v Open:%v}, want {%q %v %v %v}",
					tt.event, line.Result, line.Failed, line.Fold, line.Open,
					tt.wantResult, tt.wantFailed, tt.wantFold, tt.wantOpen)
			}
		})
	}
}

func TestEventShownCmdByMode(t *testing.T) {
	t.Parallel()

	passed := agentlog.Event{Kind: agentlog.Cmd, Done: true}
	failed := agentlog.Event{Kind: agentlog.Cmd, Done: true, ExitCode: 1}
	running := agentlog.Event{Kind: agentlog.Cmd}

	tests := []struct {
		mode  string
		event agentlog.Event
		want  bool
	}{
		{"all", passed, true},
		{"tools", passed, true},
		{"tools", running, true},
		{"skills", failed, false},
		{"fails", passed, false},
		{"fails", running, false},
		{"fails", failed, true},
	}
	for _, tt := range tests {
		if got := EventShown(tt.mode, tt.event); got != tt.want {
			t.Errorf("EventShown(%q, %+v) = %v, want %v", tt.mode, tt.event, got, tt.want)
		}
	}
}
