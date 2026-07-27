package render

import (
	"math"
	"strings"
	"testing"
)

func TestParseColor(t *testing.T) {
	for _, tc := range []struct {
		in      string
		wantErr bool
		isIndex bool
		rgb     RGB
		index   int
	}{
		{in: "#463c78", rgb: RGB{0x46, 0x3c, 0x78}},
		{in: "#000000", rgb: RGB{0, 0, 0}},
		{in: "#ffffff", rgb: RGB{255, 255, 255}},
		{in: "237", isIndex: true, index: 237},
		{in: "0", isIndex: true, index: 0},
		{in: "255", isIndex: true, index: 255},
		{in: "", wantErr: false}, // empty is "unset", not an error
		{in: "#fff", wantErr: true},
		{in: "#gggggg", wantErr: true},
		{in: "256", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "purple", wantErr: true},
	} {
		got, err := ParseColor(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseColor(%q): want error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseColor(%q): unexpected error %v", tc.in, err)
			continue
		}
		if tc.in == "" {
			if got.Set {
				t.Errorf("ParseColor(\"\"): want unset")
			}
			continue
		}
		if got.IsIndex != tc.isIndex {
			t.Errorf("ParseColor(%q): IsIndex=%v want %v", tc.in, got.IsIndex, tc.isIndex)
		}
		if tc.isIndex && got.Index != tc.index {
			t.Errorf("ParseColor(%q): Index=%d want %d", tc.in, got.Index, tc.index)
		}
		if !tc.isIndex && got.RGB != tc.rgb {
			t.Errorf("ParseColor(%q): RGB=%v want %v", tc.in, got.RGB, tc.rgb)
		}
	}
}

// An unset colour must emit nothing at all, otherwise a theme with a missing
// key would paint a black pill instead of inheriting the terminal default.
func TestUnsetColorEmitsNothing(t *testing.T) {
	var c Color
	for _, tier := range []ColorTier{ColorTrue, Color256, Color16, ColorNone} {
		if s := c.BG(tier); s != "" {
			t.Errorf("unset colour at tier %v emitted %q", tier, s)
		}
	}
}

func TestColorDowngrade(t *testing.T) {
	c, _ := ParseColor("#463c78")
	for _, tc := range []struct {
		tier   ColorTier
		prefix string
	}{
		{ColorTrue, "\x1b[48;2;"}, // exact RGB
		{Color256, "\x1b[48;5;"},  // nearest palette entry
		{Color16, "\x1b[48;5;"},   // nearest basic colour
	} {
		got := c.BG(tc.tier)
		if !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("tier %v: got %q, want prefix %q", tc.tier, got, tc.prefix)
		}
	}
	if got := c.BG(ColorNone); got != "" {
		t.Errorf("ColorNone must emit nothing, got %q", got)
	}
}

// A 16-colour downgrade must land inside 0-15, or the sequence is invalid on
// terminals that only understand the basic set.
func TestColor16StaysInRange(t *testing.T) {
	for _, hex := range []string{"#463c78", "#8a2b2b", "#3d5a45", "#ffffff", "#000000", "#e8a33d"} {
		c, _ := ParseColor(hex)
		got := c.BG(Color16)
		var n int
		if _, err := fmtSscan(got, &n); err != nil {
			t.Fatalf("%s: cannot parse %q: %v", hex, got, err)
		}
		if n < 0 || n > 15 {
			t.Errorf("%s downgraded to index %d, outside 0-15", hex, n)
		}
	}
}

func fmtSscan(seq string, n *int) (int, error) {
	// seq looks like "\x1b[48;5;NNNm"
	s := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b[48;5;"), "m")
	var v int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errNotNumeric
		}
		v = v*10 + int(r-'0')
	}
	*n = v
	return 1, nil
}

var errNotNumeric = errString("not numeric")

type errString string

func (e errString) Error() string { return string(e) }

// Contrast is what `doctor` reports and what every theme decision in this
// project was made against, so the maths must not drift.
func TestContrastWithWhite(t *testing.T) {
	for _, tc := range []struct {
		hex  string
		want float64
	}{
		{"#ffffff", 1.00},
		{"#000000", 21.00},
		{"#332a5c", 12.91}, // limits background
		{"#3a3a3a", 11.37}, // git
		{"#2d3d5a", 10.90}, // dir
	} {
		c, err := ParseColor(tc.hex)
		if err != nil {
			t.Fatalf("%s: %v", tc.hex, err)
		}
		got := c.ContrastWithWhite()
		if math.Abs(got-tc.want) > 0.02 {
			t.Errorf("%s: contrast %.2f, want %.2f", tc.hex, got, tc.want)
		}
	}
}

// Regression guard for the bug that motivated the limits redesign: severity
// text is drawn ON the pill, so it must be measured against the pill, not
// against the terminal background. These pairs were below the 4.5 AA floor
// before the background was darkened.
func TestLimitsSeverityTextIsLegible(t *testing.T) {
	bg, _ := ParseColor("#332a5c")
	for _, tc := range []struct{ name, hex string }{
		{"warn", "#e8a33d"},
		{"crit", "#ff6b6b"},
	} {
		fg, _ := ParseColor(tc.hex)
		got := contrastBetween(fg, bg)
		if got < 4.5 {
			t.Errorf("limits %s (%s on #332a5c): contrast %.2f, below the 4.5 AA floor",
				tc.name, tc.hex, got)
		}
	}
}

func contrastBetween(a, b Color) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestPaletteIsWellFormed(t *testing.T) {
	if len(palette256) != 256 {
		t.Fatalf("palette has %d entries, want 256", len(palette256))
	}
	// Spot-check the 6x6x6 cube and the greyscale ramp.
	if got := palette256[196]; got != (RGB{255, 0, 0}) {
		t.Errorf("palette[196] = %v, want pure red", got)
	}
	if got := palette256[231]; got != (RGB{255, 255, 255}) {
		t.Errorf("palette[231] = %v, want white", got)
	}
}

// isGrey reports whether a palette entry has no colour left in it.
func isGrey(c RGB) bool { return c.R == c.G && c.G == c.B }

// The bug this guards: a weighted-RGB metric sent five of the six pill
// backgrounds to the greyscale ramp, so on any 256-colour terminal -- macOS
// Terminal.app being the common one -- the bar rendered monochrome apart from
// the model pill. A coloured theme entry must stay coloured.
func TestThemeColorsDoNotDegradeToGreyAt256(t *testing.T) {
	for _, tc := range []struct{ name, hex string }{
		{"effort/low", "#444444"}, // genuinely grey, listed to prove the test can pass one
		{"effort/medium", "#005f87"}, {"effort/high", "#000075"},
		{"effort/xhigh", "#87005f"}, {"effort/max", "#b02a7a"},
		{"model/default", "#5f0087"},
		{"context/ok", "#3d5a45"}, {"context/warn", "#70551f"}, {"context/crit", "#8a2b2b"},
		{"cost", "#5b3f5c"}, {"limits", "#332a5c"},
		{"dir", "#2d3d5a"}, {"git", "#3a3a3a"},
	} {
		c, err := ParseColor(tc.hex)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := palette256[nearest256(c.RGB)]
		// A grey source may map to grey; a coloured one may not.
		if !isGrey(c.RGB) && isGrey(got) {
			t.Errorf("%s (%s) degraded to grey %v at 256 colours", tc.name, tc.hex, got)
		}
	}
}

// The pills a user sees side by side have to stay distinguishable after the
// downgrade, or the bar reads as one block. Adjacent-in-the-bar pairs only:
// the 256 cube is too sparse to promise this for every pair.
func TestAdjacentPillsStayDistinctAt256(t *testing.T) {
	idx := func(hex string) int {
		c, _ := ParseColor(hex)
		return nearest256(c.RGB)
	}
	for _, tc := range []struct{ a, b, an, bn string }{
		{"#87005f", "#3d5a45", "model/xhigh", "context/ok"},
		{"#3d5a45", "#5b3f5c", "context/ok", "cost"},
		{"#5b3f5c", "#332a5c", "cost", "limits"},
		{"#2d3d5a", "#3a3a3a", "dir", "git"},
	} {
		if ia, ib := idx(tc.a), idx(tc.b); ia == ib {
			t.Errorf("%s and %s both map to palette %d (%v) -- adjacent pills would merge",
				tc.an, tc.bn, ia, palette256[ia])
		}
	}
}

// 0-15 are remapped by the user's terminal theme, so a 256-colour match into
// that range would render differently for every colour scheme.
func TestNearest256AvoidsThemeDependentSlots(t *testing.T) {
	for _, hex := range []string{
		"#000000", "#ffffff", "#800000", "#008000", "#000080",
		"#3d5a45", "#332a5c", "#87005f", "#3a3a3a",
	} {
		c, _ := ParseColor(hex)
		if got := nearest256(c.RGB); got < 16 {
			t.Errorf("%s matched palette %d, inside the theme-dependent 0-15", hex, got)
		}
	}
}
