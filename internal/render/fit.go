package render

import (
	"strings"
	"unicode/utf8"
)

// Width is how many terminal cells s takes up once printed. SGR sequences take
// none, and East Asian wide characters take two; everything else, the private
// use icon glyphs included, is counted as one.
func Width(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if k := sgrLen(s[i:]); k > 0 {
			i += k
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		n += runeWidth(r)
		i += size
	}
	return n
}

// wide is the East Asian Wide / Fullwidth ranges that matter for branch and
// directory names. It is not all of Unicode's East_Asian_Width table; a name
// outside it is miscounted by a cell, which the margin absorbs.
var wide = [][2]rune{
	{0x1100, 0x115F}, {0x2E80, 0x303E}, {0x3041, 0x33FF}, {0x3400, 0x4DBF},
	{0x4E00, 0x9FFF}, {0xA000, 0xA4CF}, {0xAC00, 0xD7A3}, {0xF900, 0xFAFF},
	{0xFE30, 0xFE6F}, {0xFF00, 0xFF60}, {0xFFE0, 0xFFE6}, {0x1F300, 0x1F64F},
	{0x1F900, 0x1F9FF}, {0x20000, 0x3FFFD},
}

func runeWidth(r rune) int {
	switch {
	case r < 0x20, r >= 0x7f && r < 0xa0:
		return 0
	case r >= 0x300 && r <= 0x36f, r >= 0x200b && r <= 0x200f, r >= 0xfe00 && r <= 0xfe0f:
		return 0 // combining marks, zero-width spaces and joiners, variation selectors
	}
	for _, rg := range wide {
		if r >= rg[0] && r <= rg[1] {
			return 2
		}
	}
	return 1
}

// sgrLen is the length of the SGR escape sequence s begins with, or 0 if it
// does not begin with one. An unterminated sequence runs to the end of s.
func sgrLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	j := 2
	for j < len(s) && s[j] != 'm' {
		j++
	}
	if j < len(s) {
		j++
	}
	return j
}

// truncate shortens s to at most limit cells, the last of them an ellipsis.
// Escape sequences before the cut are kept so the text keeps its colours;
// restore is written ahead of the ellipsis because the cut may fall inside a
// coloured run (the git diff counts, the per-window rate-limit numbers) and
// the ellipsis belongs to the pill, not to them.
func truncate(s string, limit int, restore string) string {
	if Width(s) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	var b strings.Builder
	used := 0
	for i := 0; i < len(s); {
		if k := sgrLen(s[i:]); k > 0 {
			b.WriteString(s[i : i+k])
			i += k
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runeWidth(r)
		if used+w > limit-1 {
			break
		}
		b.WriteString(s[i : i+size])
		used += w
		i += size
	}
	b.WriteString(restore + "…")
	return b.String()
}

// fitPill draws s, shortening its text if the pill alone is wider than width.
func (o Options) fitPill(s Segment, width int) string {
	p := o.pill(s)
	if Width(p) <= width {
		return p
	}
	// What the pill costs besides its text: padding and caps.
	overhead := Width(o.pill(Segment{BG: s.BG, FG: s.FG}))
	s.Text = truncate(s.Text, max(width-overhead, 1), s.FG.FG(o.Color))
	return o.pill(s)
}

// Wrap lays segs out in rows no wider than width, breaking only between pills.
// A pill wider than a whole row is truncated rather than left to overflow.
func Wrap(segs []Segment, o Options, width int) []string {
	var rows []string
	var row strings.Builder
	rowW, inRow := 0, 0
	sepW := Width(o.Sep)
	for _, s := range segs {
		p := o.fitPill(s, width)
		w := Width(p)
		if inRow > 0 && rowW+sepW+w > width {
			rows = append(rows, row.String())
			row.Reset()
			rowW, inRow = 0, 0
		}
		if inRow > 0 {
			row.WriteString(o.Sep)
			rowW += sepW
		}
		row.WriteString(p)
		rowW += w
		inRow++
	}
	if inRow > 0 {
		rows = append(rows, row.String())
	}
	return rows
}

// Layout builds the pills at one compaction level. Level 0 is the full bar;
// each higher level gives up more detail, and the last is the tightest.
type Layout func(level int) ([]Segment, Options)

// Fit renders the most detailed level that fits width cells on one line.
// When even the tightest level does not, it wraps, using the most detailed
// level that costs no more rows than the tightest one would -- going compact
// to save a row is worth it, going compact on top of wrapping is not.
// A width of zero or less means unlimited.
func Fit(levels, width int, layout Layout) string {
	if levels < 1 {
		levels = 1
	}
	if width <= 0 {
		segs, o := layout(0)
		return Line(segs, o)
	}
	for lvl := 0; lvl < levels; lvl++ {
		segs, o := layout(lvl)
		if line := Line(segs, o); Width(line) <= width {
			return line
		}
	}
	segs, o := layout(levels - 1)
	floor := Wrap(segs, o, width)
	for lvl := 0; lvl < levels-1; lvl++ {
		s, opt := layout(lvl)
		if rows := Wrap(s, opt, width); len(rows) <= len(floor) {
			return strings.Join(rows, "\n")
		}
	}
	return strings.Join(floor, "\n")
}
