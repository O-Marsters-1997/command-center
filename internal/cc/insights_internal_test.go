package cc

import (
	"os"
	"testing"
	"time"
)

func TestSystemTimezoneNamePrefersTZEnv(t *testing.T) {
	t.Setenv("TZ", "Europe/Paris")
	if got := systemTimezoneName(); got != "Europe/Paris" {
		t.Errorf("systemTimezoneName() = %q, want Europe/Paris", got)
	}
}

func TestSystemTimezoneNameFallsBackToEtcLocaltime(t *testing.T) {
	if _, err := os.Readlink("/etc/localtime"); err != nil {
		t.Skip("no /etc/localtime symlink on this platform")
	}
	t.Setenv("TZ", "")
	if got := systemTimezoneName(); got == "" {
		t.Error(`systemTimezoneName() = "", want a name resolved from /etc/localtime`)
	}
}

func TestInsightsTimezoneUsesTheLocationsOwnNameWhenItHasOne(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	if got := insightsTimezone(paris); got != "Europe/Paris" {
		t.Errorf("insightsTimezone(Europe/Paris) = %q, want Europe/Paris", got)
	}
	if got := insightsTimezone(time.UTC); got != "UTC" {
		t.Errorf("insightsTimezone(UTC) = %q, want UTC", got)
	}
}

// TestInsightsTimezoneResolvesTimeLocalFromTheOSRatherThanUTC is the production bug this file
// exists to close: time.Local.String() is always the literal "Local", never the system's real
// zone, so the default clock's own Location must not silently read as UTC.
func TestInsightsTimezoneResolvesTimeLocalFromTheOSRatherThanUTC(t *testing.T) {
	t.Setenv("TZ", "Europe/Paris")
	if got := insightsTimezone(time.Local); got != "Europe/Paris" {
		t.Errorf("insightsTimezone(time.Local) = %q, want Europe/Paris", got)
	}
}

func TestCivilDateKeepsTheWallClockDateAtUTCMidnight(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-06-15 23:30 CEST (UTC+2) is still the 15th locally, but already the 16th in UTC.
	t.Setenv("TZ", "")
	got := civilDate(time.Date(2026, 6, 15, 23, 30, 0, 0, paris))
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("civilDate(...) = %v, want %v", got, want)
	}
}

func TestParseSinceOrDefault(t *testing.T) {
	until := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		raw  string
		want time.Time
	}{
		{"valid date", "2026-08-01", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"absent", "", until.AddDate(0, 0, -insightsDefaultRangeDays)},
		{"unparseable", "not-a-date", until.AddDate(0, 0, -insightsDefaultRangeDays)},
		{"wrong format", "20/26/09", until.AddDate(0, 0, -insightsDefaultRangeDays)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSinceOrDefault(tc.raw, until); !got.Equal(tc.want) {
				t.Errorf("parseSinceOrDefault(%q, %v) = %v, want %v", tc.raw, until, got, tc.want)
			}
		})
	}
}
