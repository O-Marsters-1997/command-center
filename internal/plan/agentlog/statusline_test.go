package agentlog_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func TestParseStatusline(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	fiveReset := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	weekReset := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		input string
		want  []agentlog.Reading
	}{
		{
			name: "both windows",
			input: `{"model":{"id":"x"},"rate_limits":{
				"five_hour":{"used_percentage":42.5,"resets_at":1794834000},
				"seven_day":{"used_percentage":7,"resets_at":1795165200}}}`,
			want: []agentlog.Reading{
				{Window: agentlog.FiveHour, Utilization: 0.425, ResetsAt: fiveReset, At: at},
				{Window: agentlog.SevenDay, Utilization: 0.07, ResetsAt: weekReset, At: at},
			},
		},
		{
			name:  "one window",
			input: `{"rate_limits":{"seven_day":{"used_percentage":0,"resets_at":1795165200}}}`,
			want:  []agentlog.Reading{{Window: agentlog.SevenDay, ResetsAt: weekReset, At: at}},
		},
		{name: "no rate limits", input: `{"model":{"id":"x"}}`},
		{name: "window without percentage", input: `{"rate_limits":{"five_hour":{"resets_at":1794834000}}}`},
		{name: "window without reset time", input: `{"rate_limits":{"five_hour":{"used_percentage":5}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentlog.ParseStatusline(strings.NewReader(tt.input), at)
			if err != nil {
				t.Fatalf("ParseStatusline() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseStatusline() mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("malformed JSON", func(t *testing.T) {
		t.Parallel()
		if _, err := agentlog.ParseStatusline(strings.NewReader(`{"rate_limits":`), at); err == nil {
			t.Error("ParseStatusline(truncated) error = nil, want error")
		}
	})
}
