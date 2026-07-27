// Package render turns resolved segments into an ANSI string.
package render

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// ColorTier is how much colour the terminal can actually display. Detected at
// startup, overridable in config -- detection is a heuristic, not a guarantee.
type ColorTier int

const (
	ColorNone ColorTier = iota // NO_COLOR set, or a dumb terminal
	Color16                    // basic ANSI
	Color256                   // xterm-256
	ColorTrue                  // 24-bit
)

func ParseColorTier(s string) (ColorTier, bool) {
	switch strings.ToLower(s) {
	case "none":
		return ColorNone, true
	case "16":
		return Color16, true
	case "256":
		return Color256, true
	case "true", "truecolor", "24bit":
		return ColorTrue, true
	}
	return ColorTrue, false
}

// NoColorSet reports whether the NO_COLOR convention is active. Surfaced by
// `doctor` so the behaviour below is discoverable rather than surprising.
func NoColorSet() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

// DetectColorTier probes COLORTERM, then TERM.
//
// It deliberately does NOT honour NO_COLOR. Claude Code sets NO_COLOR=1 in the
// environment it hands to subprocesses so that tool output stays plain, and
// the status line inherits that env -- honouring it would strip every colour
// from a bar the user installed specifically for its appearance. Users who
// genuinely want a monochrome bar set `color = "none"` in config, which is
// checked before this function is ever called.
func DetectColorTier() ColorTier {
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "dumb" {
		return ColorNone
	}
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return ColorTrue
	}
	// Windows Terminal and modern VS Code both do truecolor but often set
	// neither variable. WT_SESSION is the reliable marker for the former.
	if os.Getenv("WT_SESSION") != "" {
		return ColorTrue
	}
	if strings.Contains(term, "256") {
		return Color256
	}
	if term == "" {
		// No TERM at all usually means a non-Unix host (Windows console).
		// Assume 256 rather than stripping all colour.
		return Color256
	}
	return Color16
}

// RGB is a parsed #rrggbb colour.
type RGB struct{ R, G, B uint8 }

// Color is either a palette index or an RGB triple. Config accepts "#rrggbb"
// or a bare integer, so both forms survive to render time and get downgraded
// only once the tier is known.
type Color struct {
	RGB     RGB
	Index   int
	IsIndex bool
	Set     bool
}

func ParseColor(s string) (Color, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Color{}, nil
	}
	if strings.HasPrefix(s, "#") {
		h := strings.TrimPrefix(s, "#")
		if len(h) != 6 {
			return Color{}, fmt.Errorf("colour %q: want #rrggbb", s)
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return Color{}, fmt.Errorf("colour %q: %w", s, err)
		}
		return Color{RGB: RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, Set: true}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return Color{}, fmt.Errorf("colour %q: want #rrggbb or 0-255", s)
	}
	return Color{Index: n, IsIndex: true, Set: true}, nil
}

func (c Color) toRGB() RGB {
	if !c.IsIndex {
		return c.RGB
	}
	return palette256[c.Index]
}

// seq renders an SGR sequence for the given layer (38 fg / 48 bg), degrading
// to whatever the tier supports.
func (c Color) seq(layer int, tier ColorTier) string {
	if !c.Set || tier == ColorNone {
		return ""
	}
	switch tier {
	case ColorTrue:
		if c.IsIndex {
			return fmt.Sprintf("\x1b[%d;5;%dm", layer, c.Index)
		}
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, c.RGB.R, c.RGB.G, c.RGB.B)
	case Color256:
		if c.IsIndex {
			return fmt.Sprintf("\x1b[%d;5;%dm", layer, c.Index)
		}
		return fmt.Sprintf("\x1b[%d;5;%dm", layer, nearest256(c.RGB))
	default:
		return fmt.Sprintf("\x1b[%d;5;%dm", layer, nearest16(c.toRGB()))
	}
}

func (c Color) FG(t ColorTier) string { return c.seq(38, t) }
func (c Color) BG(t ColorTier) string { return c.seq(48, t) }

// Luminance is the WCAG relative luminance, used by the `doctor` command to
// report contrast so theme authors can't ship an unreadable pill.
func (c Color) Luminance() float64 {
	r := c.toRGB()
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(r.R) + 0.7152*f(r.G) + 0.0722*f(r.B)
}

// ContrastWithWhite returns the WCAG contrast ratio against #ffffff. 4.5 is
// AA, 7.0 is AAA; a terminal font at normal size wants AAA.
func (c Color) ContrastWithWhite() float64 {
	return 1.05 / (c.Luminance() + 0.05)
}

// nearest256 searches from 16 up, skipping the basic ANSI slots.
//
// Indices 0-15 are whatever the user's terminal theme says they are -- Solarized
// repaints all sixteen -- so matching a hex colour to one produces output whose
// appearance depends on a palette this program cannot see. Everything from 16 up
// is fixed by the xterm specification and renders the same everywhere.
func nearest256(c RGB) int {
	best, bestD := 16, math.MaxFloat64
	for i := 16; i < len(palette256); i++ {
		if d := dist(c, palette256[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// nearest16 has no such luxury: at this tier the basic sixteen are all there is.
func nearest16(c RGB) int {
	best, bestD := 0, math.MaxFloat64
	for i := 0; i < 16; i++ {
		if d := dist(c, palette256[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// dist is squared CIE76 difference in CIELAB -- distance in a space built so
// that equal steps look like equal changes.
//
// It replaced a weighted-RGB metric using the NTSC luma coefficients
// (0.30/0.59/0.11), which was wrong in a way only 256-colour terminals showed.
// Luma weights measure brightness, not colour difference: they discount blue
// about ninefold against green. The xterm palette carries 24 greys in steps of
// 10 but a colour cube in steps of 40-95, so under a brightness metric every
// dark, muted colour finds a grey nearer than any coloured entry. Five of the
// six default pill backgrounds collapsed to the same grey, turning the bar
// monochrome on macOS Terminal.app -- which reports 256 colours and no
// truecolor, so it is the one common terminal that takes this path.
func dist(a, b RGB) float64 {
	l1, a1, b1 := lab(a)
	l2, a2, b2 := lab(b)
	dl, da, db := l1-l2, a1-a2, b1-b2
	return dl*dl + da*da + db*db
}

// lab converts sRGB to CIELAB against the D65 white point.
//
// The linearisation threshold here is the sRGB specification's 0.04045, not the
// 0.03928 in Luminance. That one implements a published WCAG formula whose
// numbers doctor reports and the theme was chosen against, so it is left exactly
// as written rather than unified with this.
func lab(c RGB) (l, a, b float64) {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, bl := lin(c.R), lin(c.G), lin(c.B)
	x := (0.4124*r + 0.3576*g + 0.1805*bl) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*bl
	z := (0.0193*r + 0.1192*g + 0.9505*bl) / 1.08883
	f := func(t float64) float64 {
		if t > 216.0/24389.0 {
			return math.Cbrt(t)
		}
		return (24389.0/27.0*t + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

var palette256 = buildPalette()

func buildPalette() [256]RGB {
	var p [256]RGB
	base := []RGB{
		{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
		{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
		{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
	copy(p[:16], base)
	levels := []uint8{0, 95, 135, 175, 215, 255}
	i := 16
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				p[i] = RGB{levels[r], levels[g], levels[b]}
				i++
			}
		}
	}
	for j := 0; j < 24; j++ {
		v := uint8(8 + j*10)
		p[i] = RGB{v, v, v}
		i++
	}
	return p
}
