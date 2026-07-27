// Package segments turns a Claude Code payload plus config into pills.
package segments

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/gitinfo"
	"github.com/PouriahLabs/claude-statusline/internal/input"
	"github.com/PouriahLabs/claude-statusline/internal/render"
)

// icons[name][tier]; ASCII entries are labels rather than glyphs.
var icons = map[string][3]string{
	//        ascii       unicode   nerd
	"model":   {"Model:", "◆", "\U000F06A9"},
	"context": {"Ctx:", "▦", ""},
	"cost":    {"Cost:", "¤", ""},
	"limits":  {"Limits:", "◷", ""},
	"git":     {"Git:", "⑂", ""},
	"dir":     {"Dir:", "⌂", ""},
}

type Builder struct {
	Cfg   config.Config
	Tier  render.IconTier
	Color render.ColorTier
}

func (b Builder) icon(name string) string {
	set, ok := icons[name]
	if !ok {
		return ""
	}
	return set[b.Tier]
}

// label prefixes text with the section icon plus the configured gap.
func (b Builder) label(name, text string) string {
	ic := b.icon(name)
	if ic == "" {
		return text
	}
	gap := b.Cfg.Display.IconGap
	if b.Tier == render.IconASCII {
		gap = " " // a text label already reads as separated
	}
	return ic + gap + text
}

func (b Builder) col(s config.ColorValue) render.Color {
	c, err := render.ParseColor(string(s))
	if err != nil {
		return render.Color{}
	}
	return c
}

// Build assembles the pills named in cfg.Order, skipping any with no data.
func (b Builder) Build(p input.Payload) []render.Segment {
	text := b.col(b.Cfg.Theme.Text)
	git := gitinfo.Get(p.Dir(), b.Cfg.Display.CacheTTL())

	var out []render.Segment
	add := func(s string, bg render.Color) {
		if s != "" {
			out = append(out, render.Segment{Text: s, BG: bg, FG: text})
		}
	}

	for _, name := range b.Cfg.Order {
		switch name {
		case "model":
			t, bg := b.model(p)
			add(b.label("model", t), bg)
		case "context":
			t, bg, ok := b.context(p)
			if ok {
				add(b.label("context", t), bg)
			}
		case "cost":
			if t := b.cost(p); t != "" {
				add(b.label("cost", t), b.col(b.Cfg.Theme.Cost))
			}
		case "limits":
			if t := b.limits(p, text); t != "" {
				add(b.label("limits", t), b.col(b.Cfg.Theme.Limits))
			}
		case "dir":
			add(b.label("dir", b.dir(p)), b.col(b.Cfg.Theme.Dir))
		case "git":
			if t := b.git(git, p, text); t != "" {
				add(b.label("git", t), b.col(b.Cfg.Theme.Git))
			}
		}
	}
	return out
}

func (b Builder) model(p input.Payload) (string, render.Color) {
	name := p.Model.DisplayName
	if name == "" {
		name = p.Model.ID
	}
	if name == "" {
		name = "Claude"
	}
	for pat, pretty := range b.Cfg.ModelNames {
		if re, err := regexp.Compile(pat); err == nil && re.MatchString(name) {
			name = pretty
			break
		}
	}
	t := b.Cfg.Theme
	bg, label := t.ModelDefault, ""
	switch strings.ToLower(p.Effort.Level) {
	case "low":
		bg, label = t.EffortLow, "Low"
	case "medium":
		bg, label = t.EffortMedium, "Med"
	case "high":
		bg, label = t.EffortHigh, "High"
	case "xhigh":
		bg, label = t.EffortXHigh, "XHigh"
	case "max":
		bg, label = t.EffortMax, "Max"
	case "":
	default:
		label = p.Effort.Level
	}
	// Ultracode forces effort to xhigh, so the marker only makes sense there;
	// showing it beside any other level would mean the two disagree. See
	// ultracode.go for why this is not read from the payload.
	if lbl := b.Cfg.Display.UltracodeLabel; lbl != "" && ultracodeActive() &&
		strings.EqualFold(p.Effort.Level, "xhigh") {
		label = label + " " + lbl
	}
	if label != "" {
		name = fmt.Sprintf("%s (%s)", name, label)
	}
	return name, b.col(bg)
}

func (b Builder) context(p input.Payload) (string, render.Color, bool) {
	if p.Context == nil {
		return "", render.Color{}, false
	}
	used := p.Context.TotalInputTokens
	size := p.Context.ContextWindowSize
	for _, w := range b.Cfg.Windows {
		if re, err := regexp.Compile(w.Match); err == nil && re.MatchString(p.Model.ID) {
			size = w.Size
			break
		}
	}
	var pct float64
	var txt string
	switch {
	case used > 0 && size > 0:
		pct = float64(used) * 100 / float64(size)
		txt = fmt.Sprintf("%d%% (%dk/%s)", int(pct), (used+500)/1000, humanSize(size))
	case p.Context.UsedPercentage > 0:
		pct = p.Context.UsedPercentage
		txt = fmt.Sprintf("%d%%", int(pct+0.5))
	default:
		return "", render.Color{}, false
	}
	t := b.Cfg.Theme
	bg := t.CtxOK
	if pct >= b.Cfg.Context.Crit {
		bg = t.CtxCrit
	} else if pct >= b.Cfg.Context.Warn {
		bg = t.CtxWarn
	}
	return txt, b.col(bg), true
}

func humanSize(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%dM", n/1_000_000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

func (b Builder) cost(p input.Payload) string {
	if p.Cost.TotalCostUSD <= 0 {
		return ""
	}
	s := fmt.Sprintf("$%.2f", p.Cost.TotalCostUSD)
	// total_duration_ms is active-work time, not wall clock, so this is a
	// burn rate while working rather than cost-since-session-opened.
	if p.Cost.TotalDurationMS > 60_000 {
		hours := float64(p.Cost.TotalDurationMS) / 3_600_000
		if hours > 0 {
			s += fmt.Sprintf(" (%.2f/h)", p.Cost.TotalCostUSD/hours)
		}
	}
	return s
}

func (b Builder) limits(p input.Payload, text render.Color) string {
	if p.RateLimits == nil {
		return ""
	}
	warn := b.col(b.Cfg.Theme.LimitsWarn)
	crit := b.col(b.Cfg.Theme.LimitsCrit)
	dim := b.col(b.Cfg.Theme.LimitsDim)
	now := time.Now()
	var parts []string
	each := func(name string, w *input.Window) {
		if w == nil {
			return
		}
		pct := int(w.UsedPercentage + 0.5)
		fg := text
		if float64(pct) >= b.Cfg.Limits.Crit {
			fg = crit
		} else if float64(pct) >= b.Cfg.Limits.Warn {
			fg = warn
		}
		// Colour each window independently: "5h critical, 7d fine" must not
		// collapse into one max() colour.
		seg := fmt.Sprintf("%s %s%d%%%s", name, fg.FG(b.Color), pct, text.FG(b.Color))

		// Countdown to reset -- see reset.go for why a static percentage needs
		// this to be readable.
		if showReset(b.Cfg.Display.LimitsReset, float64(pct), b.Cfg.Limits.Warn) {
			if rem := formatReset(w.ResetsAt, now); rem != "" {
				seg += fmt.Sprintf(" %s·%s%s", dim.FG(b.Color), rem, text.FG(b.Color))
			}
		}
		parts = append(parts, seg)
	}
	each("5h", p.RateLimits.FiveHour)
	each("7d", p.RateLimits.SevenDay)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

func (b Builder) dir(p input.Payload) string {
	dir := p.Dir()
	if dir == "" {
		return "?"
	}
	name := filepath.Base(dir)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if equalPath(dir, home) {
			name = "~"
		}
	}
	if b.Cfg.Display.DirLabel && b.Tier != render.IconASCII {
		return "Dir: " + name
	}
	return name
}

func (b Builder) git(g gitinfo.Info, p input.Payload, text render.Color) string {
	add, del := p.Cost.LinesAdded, p.Cost.LinesRemoved
	var diff string
	if add != 0 || del != 0 {
		diff = fmt.Sprintf("%s+%d%s -%d%s",
			b.col(b.Cfg.Theme.DiffAdd).FG(b.Color), add,
			b.col(b.Cfg.Theme.DiffDel).FG(b.Color), del,
			text.FG(b.Color))
	}
	if !g.IsRepo || g.Branch == "" {
		// Outside a repo the diff would otherwise be dropped entirely.
		return diff
	}
	s := g.Branch
	if g.Dirty {
		s += "*"
	}
	if g.Ahead > 0 {
		s += fmt.Sprintf(" ↑%d", g.Ahead)
	}
	if g.Behind > 0 {
		s += fmt.Sprintf(" ↓%d", g.Behind)
	}
	if diff != "" {
		s += "  " + diff
	}
	return s
}

func equalPath(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(a, `/\`), strings.TrimRight(b, `/\`))
}
