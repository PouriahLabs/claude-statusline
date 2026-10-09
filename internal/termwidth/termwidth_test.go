package termwidth

import "testing"

func TestFromEnv(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want int
	}{
		{"120", 120},
		{"80", 80},
		{"", 0},
		{"0", 0},
		{"-5", 0},
		{"wide", 0},
	} {
		got, src := fromEnv(func(string) string { return tc.val })
		if got != tc.want {
			t.Errorf("COLUMNS=%q: got %d, want %d", tc.val, got, tc.want)
		}
		if (got > 0) != (src != "") {
			t.Errorf("COLUMNS=%q: source %q inconsistent with width %d", tc.val, src, got)
		}
	}
}

// Detect must never panic or report a negative width, whatever the host has
// attached: CI runners have no controlling terminal at all.
func TestDetectIsSane(t *testing.T) {
	if cols, _ := Detect(); cols < 0 {
		t.Errorf("Detect() = %d", cols)
	}
}
