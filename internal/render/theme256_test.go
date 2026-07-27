package render

import (
	"testing"

	"github.com/PouriahLabs/claude-statusline/internal/config"
)

// lightnessOf is CIELAB L*, the axis a viewer reads as "how dark is this pill".
func lightnessOf(t *testing.T, hex string) float64 {
	t.Helper()
	c, err := ParseColor(hex)
	if err != nil {
		t.Fatalf("bad colour %q: %v", hex, err)
	}
	rgb := c.RGB
	if c.IsIndex {
		rgb = palette256[c.Index]
	}
	l, _, _ := lab(rgb)
	return l
}

// The complaint this guards: on a 256-colour terminal the pills came out
// noticeably lighter than the theme they stand for. The palette's colour cube
// steps 0, 95, 135, and every shipped colour sits between 42 and 92 -- inside
// that first gap -- so nearest-match rounds every one of them upward. Limits
// drifted 23 units of L*, and the bar stopped reading as dark.
//
// A tolerance rather than zero: an exact match is often impossible, and a pill
// landing a little light is not what made this visible.
func TestShippedThemeDoesNotGetLighterAt256(t *testing.T) {
	const tolerance = 8.0 // L* units

	cfg := config.Default()
	truecolor, at256 := cfg.Theme, cfg.Resolve(true)

	pills := []struct {
		name string
		get  func(config.Theme) config.ColorValue
	}{
		{"effort_low", func(t config.Theme) config.ColorValue { return t.EffortLow }},
		{"effort_medium", func(t config.Theme) config.ColorValue { return t.EffortMedium }},
		{"effort_high", func(t config.Theme) config.ColorValue { return t.EffortHigh }},
		{"effort_xhigh", func(t config.Theme) config.ColorValue { return t.EffortXHigh }},
		{"effort_max", func(t config.Theme) config.ColorValue { return t.EffortMax }},
		{"model_default", func(t config.Theme) config.ColorValue { return t.ModelDefault }},
		{"ctx_ok", func(t config.Theme) config.ColorValue { return t.CtxOK }},
		{"ctx_warn", func(t config.Theme) config.ColorValue { return t.CtxWarn }},
		{"ctx_crit", func(t config.Theme) config.ColorValue { return t.CtxCrit }},
		{"cost", func(t config.Theme) config.ColorValue { return t.Cost }},
		{"limits", func(t config.Theme) config.ColorValue { return t.Limits }},
		{"git", func(t config.Theme) config.ColorValue { return t.Git }},
		// Dir is deliberately absent. It wants a dark blue-grey and the palette
		// has exactly two dark blues, both spent on effort_high and limits;
		// everything else in its lightness range is a grey, which would collide
		// with Git and drop the hue entirely. It draws about 16 L* light. Adding
		// it here would mean either a failing suite or a tolerance loose enough
		// to stop catching the drift this exists to catch.
	}

	for _, p := range pills {
		want := lightnessOf(t, string(p.get(truecolor)))

		// What the bar actually draws: the override when there is one, and
		// whatever nearest-match picks when there is not.
		var got float64
		c, err := ParseColor(string(p.get(at256)))
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		if c.IsIndex {
			got, _, _ = lab(palette256[c.Index])
		} else {
			got, _, _ = lab(palette256[nearest256(c.RGB)])
		}

		if got-want > tolerance {
			t.Errorf("%s: L* %.0f at 256 colours vs %.0f in truecolor (+%.0f) -- the pill reads lighter than the theme",
				p.name, got, want, got-want)
		}
	}
}
