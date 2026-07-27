// Package config loads user settings and ships the built-in themes.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

type Display struct {
	Icons    string `toml:"icons"`     // nerd | unicode | ascii
	Caps     string `toml:"caps"`      // round | arrow | block | none
	Color    string `toml:"color"`     // auto | true | 256 | 16 | none
	IconGap  string `toml:"icon_gap"`  // spaces between icon and text
	Pad      string `toml:"pad"`       // inside each pill
	Sep      string `toml:"sep"`       // between pills
	GitCache string `toml:"git_cache"` // e.g. "2s"; "0" disables
	DirLabel bool   `toml:"dir_label"` // render "Dir: ~" vs just "~"
	// UltracodeLabel is appended to the effort label when ultracode is
	// detected. Empty disables it. See internal/segments/ultracode.go --
	// detection is limited because the payload does not expose the flag.
	UltracodeLabel string `toml:"ultracode_label"`
	// LimitsReset controls the reset countdown on the quota pill:
	// "never" | "warn" (default, only once a window is elevated) | "always".
	LimitsReset string `toml:"limits_reset"`
}

// Thresholds are percentages at which a meter escalates.
type Thresholds struct {
	Warn float64 `toml:"warn"`
	Crit float64 `toml:"crit"`
}

// ColorValue is one theme colour: "#rrggbb", or a 0-255 palette index.
//
// It exists because TOML has a real integer type. The README offers both forms,
// but a plain string field rejects `dir = 24` with a type error -- and since
// Load returns the defaults on any parse failure, one unquoted index silently
// discarded the user's entire theme. Accepting both is what was documented all
// along.
type ColorValue string

func (c *ColorValue) UnmarshalTOML(v any) error {
	switch t := v.(type) {
	case string:
		*c = ColorValue(t)
	case int64:
		if t < 0 || t > 255 {
			return fmt.Errorf("palette index %d is outside 0-255", t)
		}
		*c = ColorValue(strconv.FormatInt(t, 10))
	default:
		return fmt.Errorf(`want "#rrggbb" or a 0-255 index, got %T`, v)
	}
	return nil
}

type Theme struct {
	// Model background per effort level.
	EffortLow    ColorValue `toml:"effort_low"`
	EffortMedium ColorValue `toml:"effort_medium"`
	EffortHigh   ColorValue `toml:"effort_high"`
	EffortXHigh  ColorValue `toml:"effort_xhigh"`
	EffortMax    ColorValue `toml:"effort_max"`
	ModelDefault ColorValue `toml:"model_default"`

	CtxOK   ColorValue `toml:"ctx_ok"`
	CtxWarn ColorValue `toml:"ctx_warn"`
	CtxCrit ColorValue `toml:"ctx_crit"`

	Cost ColorValue `toml:"cost"`

	// Limits keeps ONE background; severity rides on the number colour so the
	// pill's identity never shifts and 5h/7d can differ independently.
	Limits     ColorValue `toml:"limits"`
	LimitsWarn ColorValue `toml:"limits_warn_fg"`
	LimitsCrit ColorValue `toml:"limits_crit_fg"`
	// LimitsDim is the reset countdown -- supporting detail, so dimmer than
	// the percentage it annotates.
	LimitsDim ColorValue `toml:"limits_dim_fg"`

	Dir ColorValue `toml:"dir"`
	Git ColorValue `toml:"git"`

	Text    ColorValue `toml:"text"`
	DiffAdd ColorValue `toml:"diff_add"`
	DiffDel ColorValue `toml:"diff_del"`
}

// ContextWindow overrides the window size Claude Code reports, which is
// hardcoded to 200000 even for models whose real window is 1M -- making the
// reported used_percentage roughly 5x too high.
type ContextWindow struct {
	Match string `toml:"match"`
	Size  int64  `toml:"size"`
}

type Config struct {
	Order   []string   `toml:"order"`
	Display Display    `toml:"display"`
	Context Thresholds `toml:"context"`
	Limits  Thresholds `toml:"limits"`
	Theme   Theme      `toml:"theme"`
	// Theme256 overrides individual theme colours on 256-colour terminals.
	// Only the keys you set are overridden; the rest are matched from Theme.
	//
	// It exists because nearest-match cannot invent colours the palette lacks.
	// The xterm cube steps 0, 95, 135, 175, 215, 255, and every colour in the
	// shipped theme sits in the 42-92 range -- inside that first gap, so they
	// all resolve upward and converge. Two can land on the same entry, which is
	// a design decision the author should get to make rather than arithmetic.
	Theme256   Theme             `toml:"theme_256"`
	Windows    []ContextWindow   `toml:"context_window"`
	ModelNames map[string]string `toml:"model_names"`
}

// Resolve returns the theme to render with at the given colour tier, applying
// the Theme256 overrides only where one is set and only at 256 colours.
func (c Config) Resolve(is256 bool) Theme {
	if !is256 {
		return c.Theme
	}
	t := c.Theme
	rt, ro := reflect.ValueOf(&t).Elem(), reflect.ValueOf(c.Theme256)
	for i := 0; i < rt.NumField(); i++ {
		if v := ro.Field(i).String(); v != "" {
			rt.Field(i).SetString(v)
		}
	}
	return t
}

func Default() Config {
	return Config{
		Order: []string{"model", "context", "cost", "limits", "dir", "git"},
		Display: Display{
			Icons: "nerd", Caps: "round", Color: "auto",
			IconGap: "  ", Pad: " ", Sep: " ",
			GitCache: "2s", DirLabel: true, UltracodeLabel: "Ultra",
			LimitsReset: "warn",
		},
		Context: Thresholds{Warn: 50, Crit: 80},
		Limits:  Thresholds{Warn: 50, Crit: 80},
		Theme: Theme{
			// Grey -> blue -> violet -> magenta -> pink. Deliberately no amber
			// or red: those are the alarm colours owned by the two meters, and
			// Model sits directly beside Context. Effort is a mode, not a
			// warning.
			// EffortHigh is royal blue, not violet. #5a3f8a measured 15.3
			// perceptual distance to the cost pill's plum -- effectively the
			// same colour on a bar that already carries slate, indigo and
			// plum. This scores 43.5 against the nearest always-visible pill,
			// and 73 against EffortMedium so switching effort still visibly
			// changes something.
			EffortLow: "#444444", EffortMedium: "#005f87", EffortHigh: "#000075",
			EffortXHigh: "#87005f", EffortMax: "#b02a7a", ModelDefault: "#5f0087",

			// OK is muted so it blends; warn and crit get louder, because an
			// alarm that harmonises with the palette is a broken alarm.
			// Contrast vs white: 7.65 / 6.98 / 8.53 (WCAG AAA is 7.0).
			CtxOK: "#3d5a45", CtxWarn: "#70551f", CtxCrit: "#8a2b2b",

			Cost: "#5b3f5c",

			// Background darkened from #463c78. The severity text sits ON
			// this, and against the old value the amber/red measured 4.47 and
			// 3.47 -- both under the 4.5 AA floor, so a recoloured percentage
			// became HARDER to read. Fixed by darkening the background rather
			// than lightening the text: pastel severity colours cleared the
			// contrast bar but stopped reading as an alarm. Now 5.99 / 4.65.
			// Measure any replacement against Limits, not the terminal bg.
			Limits: "#332a5c", LimitsWarn: "#e8a33d", LimitsCrit: "#ff6b6b",
			LimitsDim: "#9a93c4",

			Dir: "#2d3d5a", Git: "#3a3a3a",

			Text: "#ffffff", DiffAdd: "#87d787", DiffDel: "#ff5f5f",
		},
		// Nearest-match minimises total colour difference, which is the wrong
		// objective here: it will happily buy hue accuracy with lightness, and
		// the palette gives it every reason to. The xterm cube steps 0, 95, 135,
		// while every colour above sits between 42 and 92 -- inside that first
		// gap, so each one rounds up. Limits drifted 23 units lighter, Cost 15.
		// The bar stopped reading as dark muted pills, which is what it is.
		//
		// These are picked to hold lightness instead, accepting a more saturated
		// hue in exchange, since the palette has no dark muted entries at all --
		// only near-black, mid greys, and saturated darks built from 0 and 95.
		// Measured drift in L*, before -> after:
		//
		//	effort_high  +3  -> -4
		//	ctx_warn     +5  -> +1
		//	cost        +15  -> -10
		//	limits      +23  -> -6
		//
		// Dir is the one that cannot be helped. It wants a dark blue-grey, and
		// the only dark blues are 17 and 18, both spent above; everything else
		// in range is a grey, which would collide with Git and lose the hue
		// entirely. It keeps its nearest match and stays lighter than it should.
		//
		// This also settles the collision these overrides began as: Limits and
		// Dir both matched entry 60, and they sit next to each other in the bar.
		Theme256: Theme{
			EffortHigh: "17",
			CtxWarn:    "58",
			Cost:       "53",
			Limits:     "18",
		},
		Windows: []ContextWindow{
			{Match: `^claude-(opus|sonnet|fable|mythos)-(5|4-[6-9])`, Size: 1000000},
			{Match: `^claude-haiku-4-5`, Size: 200000},
		},
		ModelNames: map[string]string{
			`^claude-opus-5`:     "Opus 5",
			`^claude-sonnet-5`:   "Sonnet 5",
			`^claude-fable-5`:    "Fable 5",
			`^claude-mythos-5`:   "Mythos 5",
			`^claude-opus-4-8`:   "Opus 4.8",
			`^claude-opus-4-7`:   "Opus 4.7",
			`^claude-opus-4-6`:   "Opus 4.6",
			`^claude-opus-4-5`:   "Opus 4.5",
			`^claude-sonnet-4-6`: "Sonnet 4.6",
			`^claude-sonnet-4-5`: "Sonnet 4.5",
			`^claude-haiku-4-5`:  "Haiku 4.5",
		},
	}
}

// Dir is the platform config directory for this tool.
func Dir() string {
	if v := os.Getenv("CLAUDE_STATUSLINE_CONFIG_DIR"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-statusline")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

// Load merges the user's config over the defaults. A missing file is not an
// error -- the tool must render with zero setup.
func Load(path string) (Config, error) {
	c := Default()
	if path == "" {
		path = Path()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	// Decoding into the pre-populated struct leaves unset keys at their
	// defaults, so a user only writes what they want to change.
	if err := toml.Unmarshal(b, &c); err != nil {
		return Default(), err
	}
	return c, nil
}

func (d Display) CacheTTL() time.Duration {
	if d.GitCache == "" {
		return 2 * time.Second
	}
	v, err := time.ParseDuration(d.GitCache)
	if err != nil {
		return 2 * time.Second
	}
	return v
}
