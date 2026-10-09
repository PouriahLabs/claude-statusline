package render

import (
	"strings"
	"testing"
)

func TestWidth(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"\x1b[38;2;1;2;3mabc\x1b[0m", 3},
		{"◆ ▦ ¤", 5},
		{"\U000F06A9 x", 3}, // astral private-use icon is one cell, not four bytes
		{"日本語", 6},          // East Asian wide
		{"é", 1},           // combining accent adds no cell
		{"a\x1b[1;31", 1},   // unterminated escape swallows the rest, as stripSGR does
		{"a\x1b[31mb\x1b[0m", 2},
	} {
		if got := Width(tc.in); got != tc.want {
			t.Errorf("Width(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10, ""); got != "short" {
		t.Errorf("fits: got %q", got)
	}
	if got := truncate("abcdefghij", 5, ""); got != "abcd…" {
		t.Errorf("plain: got %q", got)
	}
	if got := truncate("abcdef", 0, ""); got != "" {
		t.Errorf("zero limit: got %q", got)
	}
	// A cut inside a coloured run keeps the colour up to the cut, then hands the
	// ellipsis back to the pill's own colour.
	in := "ab\x1b[31mcdefgh\x1b[0m"
	got := truncate(in, 5, "\x1b[37m")
	if want := "ab\x1b[31mcd\x1b[37m…"; got != want {
		t.Errorf("sgr: got %q, want %q", got, want)
	}
	if Width(got) != 5 {
		t.Errorf("sgr: width %d, want 5", Width(got))
	}
	// A wide rune that would straddle the limit is dropped, not split.
	if got := truncate("日本語です", 4, ""); Width(got) > 4 {
		t.Errorf("wide: %q is %d cells", got, Width(got))
	}
}

func testOpts() Options {
	return Options{Color: ColorTrue, Caps: CapNone, Pad: " ", Sep: " ", Reset: "\x1b[0m"}
}

func pills(texts ...string) []Segment {
	out := make([]Segment, len(texts))
	for i, s := range texts {
		out[i] = Segment{Text: s}
	}
	return out
}

func TestWrapBreaksBetweenPills(t *testing.T) {
	// Each pill is text+2 wide. 10 + 1 + 10 = 21 fits in 21; a third does not.
	rows := Wrap(pills("12345678", "12345678", "12345678"), testOpts(), 21)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %q", len(rows), rows)
	}
	if Width(rows[0]) != 21 || Width(rows[1]) != 10 {
		t.Errorf("row widths %d, %d; want 21, 10", Width(rows[0]), Width(rows[1]))
	}
}

func TestWrapTruncatesAPillWiderThanTheRow(t *testing.T) {
	rows := Wrap(pills("short", strings.Repeat("x", 50)), testOpts(), 20)
	for i, r := range rows {
		if Width(r) > 20 {
			t.Errorf("row %d is %d cells: %q", i, Width(r), r)
		}
	}
	if len(rows) != 2 || !strings.Contains(rows[1], "…") {
		t.Errorf("rows = %q, want the long pill alone on row 2, ellipsised", rows)
	}
}

func TestWrapEmpty(t *testing.T) {
	if rows := Wrap(nil, testOpts(), 40); len(rows) != 0 {
		t.Errorf("got %q", rows)
	}
}

// levels fakes a bar that shrinks as the level rises.
func levels(widths ...int) Layout {
	return func(level int) ([]Segment, Options) {
		w := widths[level]
		// Two pills of equal size, so the whole line is 2*(w+2) + 1.
		half := strings.Repeat("x", w/2)
		return pills(half, half), testOpts()
	}
}

func TestFitUnlimitedDrawsLevelZero(t *testing.T) {
	got := Fit(3, 0, levels(60, 40, 20))
	if strings.Contains(got, "\n") || Width(got) != 2*(30+2)+1 {
		t.Errorf("width %d, rows %q", Width(got), got)
	}
}

func TestFitPicksTheMostDetailedLevelThatFits(t *testing.T) {
	// Lines are 65, 45 and 25 cells.
	for _, tc := range []struct{ width, want int }{
		{200, 65}, {65, 65}, {64, 45}, {45, 45}, {44, 25}, {25, 25},
	} {
		got := Fit(3, tc.width, levels(60, 40, 20))
		if strings.Contains(got, "\n") || Width(got) != tc.want {
			t.Errorf("width %d: got %d cells over %d rows, want %d on one",
				tc.width, Width(got), strings.Count(got, "\n")+1, tc.want)
		}
	}
}

func TestFitWrapsOnlyWhenNothingFits(t *testing.T) {
	got := Fit(3, 20, levels(60, 40, 20))
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %q", len(rows), got)
	}
	for _, r := range rows {
		if Width(r) > 20 {
			t.Errorf("row %q is %d cells", r, Width(r))
		}
	}
}

// Wrapping should not also throw detail away that the extra row already paid
// for: every level here wraps to two rows at width 40, so level 0 wins.
func TestFitWrapsAtTheMostDetailedLevelThatCostsNoExtraRow(t *testing.T) {
	got := Fit(3, 40, levels(68, 56, 44))
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("got %d rows: %q", len(rows), got)
	}
	// Level 0 pills are 36 cells; level 2 pills are 24.
	if Width(rows[0]) != 36 {
		t.Errorf("row 0 is %d cells, want the level-0 pill (36)", Width(rows[0]))
	}
}

// Whatever the width, no row may exceed it.
func TestFitNeverOverflows(t *testing.T) {
	segs := func(level int) ([]Segment, Options) {
		o := testOpts()
		o.Caps = CapRound
		return pills(strings.Repeat("a", 30-level*5), strings.Repeat("b", 18), "日本語日本語日本語", "c"), o
	}
	for width := 12; width < 120; width++ {
		for i, row := range strings.Split(Fit(4, width, segs), "\n") {
			if Width(row) > width {
				t.Fatalf("width %d: row %d is %d cells: %q", width, i, Width(row), row)
			}
		}
	}
}
