package segments

import (
	"fmt"
	"time"
)

// formatReset renders the time until a unix epoch as a compact duration:
// 45m, 2h15m, 6d3h. It returns "" when the stamp is missing or already past,
// so callers can append unconditionally.
//
// Why this exists: the rate-limit percentages in the payload are integers, and
// a single turn is often well under 1% of a 5-hour budget. The number can
// therefore sit unchanged for many turns and look stale or broken. It isn't --
// there is simply nothing new to report. The countdown is what makes a static
// value interpretable: 64% with five hours to go is a problem, 64% with eight
// minutes to go is not.
//
// now is a parameter rather than a call to time.Now so this is testable.
func formatReset(epoch int64, now time.Time) string {
	if epoch <= 0 {
		return ""
	}
	d := time.Unix(epoch, 0).Sub(now)
	if d <= 0 {
		return ""
	}

	// Round to whole minutes first and carry upward, so a span a hair under an
	// hour reads "1h" rather than "60m".
	mins := int((d + 30*time.Second) / time.Minute)
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	hours, m := mins/60, mins%60
	if hours < 24 {
		if m == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, m)
	}
	days, h := hours/24, hours%24
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, h)
}

// showReset decides whether to render the countdown for a window at pct.
//   - "never"  : off
//   - "warn"   : only once the window is at or above the warn threshold, so
//     the bar stays short while there is nothing to worry about
//   - "always" : both windows, always
func showReset(mode string, pct, warn float64) bool {
	switch mode {
	case "never", "":
		return false
	case "always":
		return true
	default: // "warn"
		return pct >= warn
	}
}
