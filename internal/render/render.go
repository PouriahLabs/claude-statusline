package render

import "strings"

// IconTier is how much of the glyph range the terminal font can draw.
// There is no reliable way to probe this, so it is chosen by the install
// wizard (which shows the user glyphs and asks) and stored in config.
type IconTier int

const (
	IconASCII   IconTier = iota // "Model:" text labels, works literally everywhere
	IconUnicode                 // ◆ ▦ ⌂ -- BMP symbols, most fonts have them
	IconNerd                    // Nerd Font private-use glyphs
)

func ParseIconTier(s string) (IconTier, bool) {
	switch strings.ToLower(s) {
	case "ascii":
		return IconASCII, true
	case "unicode":
		return IconUnicode, true
	case "nerd":
		return IconNerd, true
	}
	return IconASCII, false
}

// CapTier controls the pill end-caps.
type CapTier int

const (
	CapNone  CapTier = iota // plain, just background padding
	CapBlock                // ▐ ▌ half blocks, present in most fonts
	CapRound                // powerline rounded, needs a patched font
	CapArrow                // powerline triangles, needs a patched font
)

func ParseCapTier(s string) (CapTier, bool) {
	switch strings.ToLower(s) {
	case "none":
		return CapNone, true
	case "block":
		return CapBlock, true
	case "round":
		return CapRound, true
	case "arrow":
		return CapArrow, true
	}
	return CapNone, false
}

func (c CapTier) glyphs() (string, string) {
	switch c {
	case CapRound:
		return "", ""
	case CapArrow:
		return "", ""
	case CapBlock:
		return "▐", "▌"
	}
	return "", ""
}

// Segment is one resolved pill: already-formatted text plus its colours.
// Text may contain embedded SGR sequences (the git diff counts and the
// per-window rate-limit numbers both colour parts of themselves), so anything
// that emits into Text must restore the pill's foreground afterwards.
type Segment struct {
	Text string
	BG   Color
	FG   Color
}

type Options struct {
	Color   ColorTier
	Caps    CapTier
	Pad     string // inside the pill, each side
	Sep     string // between pills
	Reset   string
	Enabled bool
}

func DefaultOptions() Options {
	return Options{
		Color: DetectColorTier(),
		Caps:  CapNone,
		Pad:   " ",
		Sep:   " ",
		Reset: "\x1b[0m",
	}
}

// Line renders all segments into a single status line.
func Line(segs []Segment, o Options) string {
	var b strings.Builder
	capL, capR := o.Caps.glyphs()
	for i, s := range segs {
		if i > 0 {
			b.WriteString(o.Sep)
		}
		if o.Color == ColorNone {
			// Strip embedded SGR from Text too -- a NO_COLOR consumer means it.
			b.WriteString(stripSGR(s.Text))
			continue
		}
		// The caps are drawn in the fill colour against the terminal's own
		// background; that is what makes the pill look detached rather than
		// part of a continuous bar.
		if capL != "" {
			b.WriteString(s.BG.FG(o.Color) + capL)
		}
		b.WriteString(s.BG.BG(o.Color))
		b.WriteString(s.FG.FG(o.Color))
		b.WriteString(o.Pad + s.Text + o.Pad)
		b.WriteString(o.Reset)
		if capR != "" {
			b.WriteString(s.BG.FG(o.Color) + capR + o.Reset)
		}
	}
	return b.String()
}

func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
