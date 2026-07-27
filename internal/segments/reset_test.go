package segments

import (
	"testing"
	"time"
)

func TestFormatReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name string
		in   time.Duration
		want string
	}{
		{"under a minute rounds up", 40 * time.Second, "1m"},
		{"minutes", 45 * time.Minute, "45m"},
		{"59 minutes stays minutes", 59 * time.Minute, "59m"},
		// The reason rounding happens before the carry: a span a hair under
		// an hour must read "1h", never "60m".
		{"just under an hour carries", 59*time.Minute + 45*time.Second, "1h"},
		{"exactly an hour", time.Hour, "1h"},
		{"hours and minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
		{"whole hours drop minutes", 5 * time.Hour, "5h"},
		{"just under a day carries", 23*time.Hour + 59*time.Minute + 45*time.Second, "1d"},
		{"days and hours", 6*24*time.Hour + 7*time.Hour, "6d7h"},
		{"whole days drop hours", 3 * 24 * time.Hour, "3d"},
	} {
		got := formatReset(now.Add(tc.in).Unix(), now)
		if got != tc.want {
			t.Errorf("%s: formatReset(+%v) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// A missing or already-elapsed stamp must yield "", so callers can append it
// unconditionally without printing a stale or negative countdown.
func TestFormatResetEmptyCases(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name  string
		epoch int64
	}{
		{"zero", 0},
		{"negative", -1},
		{"already past", now.Add(-time.Hour).Unix()},
		{"exactly now", now.Unix()},
	} {
		if got := formatReset(tc.epoch, now); got != "" {
			t.Errorf("%s: got %q, want empty", tc.name, got)
		}
	}
}

func TestShowReset(t *testing.T) {
	const warn = 50.0
	for _, tc := range []struct {
		mode string
		pct  float64
		want bool
	}{
		{"never", 99, false},
		{"", 99, false}, // unset behaves as off
		{"always", 0, true},
		{"always", 99, true},
		{"warn", 49, false},
		{"warn", 50, true}, // at the threshold
		{"warn", 91, true},
		{"nonsense", 91, true}, // unknown values fall back to warn
		{"nonsense", 10, false},
	} {
		if got := showReset(tc.mode, tc.pct, warn); got != tc.want {
			t.Errorf("showReset(%q, %.0f) = %v, want %v", tc.mode, tc.pct, got, tc.want)
		}
	}
}
