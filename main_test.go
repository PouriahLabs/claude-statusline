package main

import (
	"strings"
	"testing"

	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/render"
)

func fitted(width int, icons, caps string) string {
	cfg := config.Default()
	cfg.Display.Icons, cfg.Display.Caps, cfg.Display.Color = icons, caps, "true"
	cfg.Display.Width = width
	return build(cfg, samplePayload())
}

// The point of the feature: at any width, nothing is clipped by the terminal.
func TestBuildFitsEveryWidth(t *testing.T) {
	for _, tier := range []struct{ icons, caps string }{
		{"nerd", "round"}, {"unicode", "block"}, {"ascii", "none"},
	} {
		for width := 20; width <= 200; width++ {
			for i, row := range strings.Split(fitted(width, tier.icons, tier.caps), "\n") {
				if w := render.Width(row); w > width {
					t.Fatalf("%s/%s at %d: row %d is %d cells: %q", tier.icons, tier.caps, width, i, w, row)
				}
			}
		}
	}
}

func TestBuildDegradesBeforeItWraps(t *testing.T) {
	full := fitted(-1, "nerd", "round")
	if strings.Contains(full, "\n") {
		t.Fatalf("width = -1 must never wrap: %q", full)
	}
	if got := fitted(render.Width(full), "nerd", "round"); got != full {
		t.Errorf("a bar that fits exactly must be drawn in full")
	}
	// One cell short drops detail but stays on a single line.
	short := fitted(render.Width(full)-1, "nerd", "round")
	if strings.Contains(short, "\n") {
		t.Errorf("one cell short should compact, not wrap: %q", short)
	}
	if render.Width(short) >= render.Width(full) {
		t.Errorf("compact bar is %d cells, full is %d", render.Width(short), render.Width(full))
	}
	// Squeezed hard enough, it wraps.
	if rows := strings.Split(fitted(50, "nerd", "round"), "\n"); len(rows) < 2 {
		t.Errorf("a 50-cell bar should wrap, got %d row", len(rows))
	}
}

func TestBudget(t *testing.T) {
	cfg := config.Default()
	cfg.Display.Width = -1
	if n, _ := budget(cfg); n != 0 {
		t.Errorf("width = -1: budget %d, want 0 (unlimited)", n)
	}
	cfg.Display.Width = 90
	if n, _ := budget(cfg); n != 90 {
		t.Errorf("explicit width is used exactly: got %d, want 90", n)
	}
}
