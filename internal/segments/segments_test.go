package segments

import (
	"strings"
	"testing"

	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/input"
	"github.com/PouriahLabs/claude-statusline/internal/render"
)

func builder() Builder {
	return Builder{Cfg: config.Default(), Tier: render.IconASCII, Color: render.ColorNone}
}

func TestModelNaming(t *testing.T) {
	for _, tc := range []struct{ id, effort, want string }{
		{"claude-opus-5", "xhigh", "Opus 5 (XHigh)"},
		{"claude-opus-5", "max", "Opus 5 (Max)"},
		{"claude-opus-5", "", "Opus 5"},
		{"claude-sonnet-4-6", "high", "Sonnet 4.6 (High)"},
		{"claude-haiku-4-5", "low", "Haiku 4.5 (Low)"},
		// An unrecognised model must pass through rather than vanish.
		{"claude-future-9", "medium", "claude-future-9 (Med)"},
		// An unrecognised effort must pass through verbatim, so a new level
		// added by Claude Code still renders.
		{"claude-opus-5", "brand-new", "Opus 5 (brand-new)"},
		{"", "", "Claude"},
	} {
		got, _ := builder().model(input.Payload{
			Model:  input.Model{ID: tc.id},
			Effort: input.Effort{Level: tc.effort},
		})
		if got != tc.want {
			t.Errorf("model(%q, %q) = %q, want %q", tc.id, tc.effort, got, tc.want)
		}
	}
}

// The whole reason this tool exists: Claude Code reports a 200k window for
// models whose real window is 1M, overstating usage ~5x.
func TestContextWindowCorrection(t *testing.T) {
	for _, tc := range []struct {
		id     string
		tokens int64
		want   string
	}{
		{"claude-opus-5", 406_000, "40% (406k/1M)"},
		{"claude-sonnet-5", 100_000, "10% (100k/1M)"},
		{"claude-haiku-4-5", 100_000, "50% (100k/200k)"},
		// Unknown model falls back to the reported size, so the correction
		// can never make things worse than the status quo.
		{"claude-future-9", 100_000, "50% (100k/200k)"},
	} {
		got, _, ok := builder().context(input.Payload{
			Model:   input.Model{ID: tc.id},
			Context: &input.Context{TotalInputTokens: tc.tokens, ContextWindowSize: 200_000},
		})
		if !ok {
			t.Errorf("context(%q): not ok", tc.id)
			continue
		}
		if got != tc.want {
			t.Errorf("context(%q, %d) = %q, want %q", tc.id, tc.tokens, got, tc.want)
		}
	}
}

func TestContextThresholds(t *testing.T) {
	b := builder()
	for _, tc := range []struct {
		tokens int64
		want   string
	}{
		{400_000, string(b.Cfg.Theme.CtxOK)},   // 40%
		{620_000, string(b.Cfg.Theme.CtxWarn)}, // 62%
		{880_000, string(b.Cfg.Theme.CtxCrit)}, // 88%
		{500_000, string(b.Cfg.Theme.CtxWarn)}, // exactly at the warn boundary
		{800_000, string(b.Cfg.Theme.CtxCrit)}, // exactly at the crit boundary
	} {
		_, bg, _ := b.context(input.Payload{
			Model:   input.Model{ID: "claude-opus-5"},
			Context: &input.Context{TotalInputTokens: tc.tokens, ContextWindowSize: 200_000},
		})
		want, _ := render.ParseColor(tc.want)
		if bg != want {
			t.Errorf("%d tokens: bg = %+v, want %+v (%s)", tc.tokens, bg, want, tc.want)
		}
	}
}

func TestContextMissing(t *testing.T) {
	if _, _, ok := builder().context(input.Payload{}); ok {
		t.Error("context with no payload data should not render")
	}
}

// Each rate-limit window is coloured independently: "5h critical, 7d fine"
// must not collapse into one max() colour.
func TestLimitsColourWindowsIndependently(t *testing.T) {
	b := builder()
	b.Color = render.ColorTrue // colours only emit when a tier is active
	text, _ := render.ParseColor(string(b.Cfg.Theme.Text))
	crit, _ := render.ParseColor(string(b.Cfg.Theme.LimitsCrit))

	got := b.limits(input.Payload{RateLimits: &input.RateLimits{
		FiveHour: &input.Window{UsedPercentage: 91},
		SevenDay: &input.Window{UsedPercentage: 18},
	}}, text)

	critSeq := crit.FG(render.ColorTrue)
	if strings.Count(got, critSeq) != 1 {
		t.Errorf("expected exactly one critical-coloured window, got %q", got)
	}
	if !strings.Contains(got, "5h "+critSeq+"91%") {
		t.Errorf("5h should be critical: %q", got)
	}
	if strings.Contains(got, critSeq+"18%") {
		t.Errorf("7d must not inherit the critical colour: %q", got)
	}
}

func TestLimitsMissing(t *testing.T) {
	text, _ := render.ParseColor("#ffffff")
	if got := builder().limits(input.Payload{}, text); got != "" {
		t.Errorf("limits with no data should be empty, got %q", got)
	}
}

func TestCost(t *testing.T) {
	b := builder()
	if got := b.cost(input.Payload{}); got != "" {
		t.Errorf("zero cost should render nothing, got %q", got)
	}
	// Under a minute of work, no rate is shown -- it would be noise.
	got := b.cost(input.Payload{Cost: input.Cost{TotalCostUSD: 1.5, TotalDurationMS: 30_000}})
	if got != "$1.50" {
		t.Errorf("short session = %q, want $1.50", got)
	}
	got = b.cost(input.Payload{Cost: input.Cost{TotalCostUSD: 13.26, TotalDurationMS: 1_466_000}})
	if !strings.HasPrefix(got, "$13.26 (") {
		t.Errorf("long session = %q, want a rate suffix", got)
	}
}

func TestBuildSkipsEmptySegments(t *testing.T) {
	b := builder()
	segs := b.Build(input.Payload{Model: input.Model{ID: "claude-opus-5"}})
	// Model and dir always render; cost, limits and context have no data here.
	for _, s := range segs {
		if s.Text == "" {
			t.Error("Build emitted an empty segment")
		}
	}
	if len(segs) < 2 {
		t.Errorf("expected at least model and dir, got %d segments", len(segs))
	}
}

func TestBuildRespectsOrder(t *testing.T) {
	b := builder()
	b.Cfg.Order = []string{"dir", "model"}
	segs := b.Build(input.Payload{Model: input.Model{ID: "claude-opus-5"}})
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}
	if !strings.Contains(segs[0].Text, "Dir:") {
		t.Errorf("first segment should be dir, got %q", segs[0].Text)
	}
	if !strings.Contains(segs[1].Text, "Opus 5") {
		t.Errorf("second segment should be model, got %q", segs[1].Text)
	}
}
