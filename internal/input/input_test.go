package input

import (
	"strings"
	"testing"
)

// The status line must never crash or print a parse error, whatever it is
// handed. Claude Code omits whole sections early in a session, and a broken
// payload should degrade to "render less", not "render an error".
func TestParseIsTotal(t *testing.T) {
	for _, in := range []string{
		"",
		"{}",
		"   ",
		"not json at all",
		`{"model":`,
		`{"model": null}`,
		`{"context_window": null}`,
		`{"rate_limits": {"five_hour": null, "seven_day": null}}`,
		`{"cost": {"total_cost_usd": "not a number"}}`,
		`[1,2,3]`,
	} {
		p := Parse(strings.NewReader(in))
		_ = p.Dir()
		_ = p.Model.ID
		if p.Context != nil {
			_ = p.Context.TotalInputTokens
		}
		if p.RateLimits != nil && p.RateLimits.FiveHour != nil {
			_ = p.RateLimits.FiveHour.UsedPercentage
		}
	}
}

func TestParseRealPayload(t *testing.T) {
	// Captured live from Claude Code 2.1.218.
	const raw = `{"session_id":"3c9a9605","cwd":"C:\\Users\\Pouri",
	"effort":{"level":"xhigh"},
	"model":{"id":"claude-opus-5","display_name":"claude-opus-5"},
	"workspace":{"current_dir":"C:\\Users\\Pouri","project_dir":"C:\\Users\\Pouri"},
	"version":"2.1.218","output_style":{"name":"default"},
	"cost":{"total_cost_usd":13.26,"total_duration_ms":1466000,"total_lines_added":121,"total_lines_removed":55},
	"context_window":{"total_input_tokens":406000,"context_window_size":200000},
	"fast_mode":false,"thinking":{"enabled":true},
	"rate_limits":{"five_hour":{"used_percentage":86,"resets_at":1785129600},
	               "seven_day":{"used_percentage":19,"resets_at":1785661200}}}`

	p := Parse(strings.NewReader(raw))
	if p.Model.ID != "claude-opus-5" {
		t.Errorf("model.id = %q", p.Model.ID)
	}
	if p.Effort.Level != "xhigh" {
		t.Errorf("effort.level = %q", p.Effort.Level)
	}
	if p.Context == nil || p.Context.TotalInputTokens != 406000 {
		t.Errorf("context tokens not parsed: %+v", p.Context)
	}
	if p.RateLimits == nil || p.RateLimits.FiveHour == nil ||
		p.RateLimits.FiveHour.UsedPercentage != 86 {
		t.Errorf("rate limits not parsed: %+v", p.RateLimits)
	}
	if p.Cost.LinesAdded != 121 || p.Cost.LinesRemoved != 55 {
		t.Errorf("diff counts not parsed: %+v", p.Cost)
	}
}

// Dir prefers workspace.current_dir, because cwd can lag behind when the user
// changes directory mid-session.
func TestDirPrefersWorkspace(t *testing.T) {
	p := Parse(strings.NewReader(`{"cwd":"/a","workspace":{"current_dir":"/b"}}`))
	if got := p.Dir(); got != "/b" {
		t.Errorf("Dir() = %q, want /b", got)
	}
	p = Parse(strings.NewReader(`{"cwd":"/a"}`))
	if got := p.Dir(); got != "/a" {
		t.Errorf("Dir() = %q, want /a", got)
	}
	p = Parse(strings.NewReader(`{}`))
	if got := p.Dir(); got != "" {
		t.Errorf("Dir() = %q, want empty", got)
	}
}
