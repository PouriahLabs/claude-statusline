// Command claude-statusline renders a Claude Code status line.
//
// Claude Code pipes a JSON payload on stdin and prints whatever comes back on
// stdout, so the default action is: read stdin, render, write UTF-8 bytes.
//
// Output is written as raw UTF-8 straight to stdout rather than through any
// text-encoding layer. The icons are astral private-use codepoints, and any
// path that re-encodes through a legacy codepage turns each one into "??".
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PouriahLabs/claude-statusline/internal/claudecode"
	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/input"
	"github.com/PouriahLabs/claude-statusline/internal/render"
	"github.com/PouriahLabs/claude-statusline/internal/segments"
	"github.com/PouriahLabs/claude-statusline/internal/wizard"
)

var version = "dev" // set by the release build

const usage = `claude-statusline -- a status line for Claude Code.

Usage:
  claude-statusline            render a status line from the JSON on stdin
                               (this is what Claude Code runs; not for humans)
  claude-statusline init       interactive setup: icons, colours, wiring
  claude-statusline preview    render every icon tier so you can compare
  claude-statusline doctor     report config, detected tiers and contrast
  claude-statusline uninstall  unwire from Claude Code, remove config and cache
  claude-statusline version    print the version

Flags (apply to render and preview):
  --config PATH   use this config file instead of the default
  --icons TIER    nerd | unicode | ascii
  --color TIER    true | 256 | 16 | none
  --caps TIER     round | arrow | block | none

Docs: https://github.com/PouriahLabs/claude-statusline
`

func main() {
	var (
		cfgPath = flag.String("config", "", "path to config.toml")
		icons   = flag.String("icons", "", "override icon tier: nerd|unicode|ascii")
		colors  = flag.String("color", "", "override colour tier: true|256|16|none")
		caps    = flag.String("caps", "", "override pill caps: round|arrow|block|none")
		showVer = flag.Bool("version", false, "print the version and exit")
	)
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	switch flag.Arg(0) {
	case "version":
		fmt.Println(version)
		return
	case "help":
		fmt.Print(usage)
		return
	case "doctor":
		os.Exit(doctor(*cfgPath))
	case "init":
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			// Say so before the wizard offers to overwrite it: the answers it
			// carries forward are the defaults, not what is in the file, and
			// accepting the overwrite silently discards a config the user may
			// only have mistyped.
			fmt.Fprintf(os.Stderr, "\nYour existing config could not be read:\n  %v\n", err)
			fmt.Fprintf(os.Stderr, "Starting from the built-in defaults.\n")
			cfg = config.Default()
		}
		w := wizard.New(func(c config.Config) string { return build(c, samplePayload()) })
		if _, err := w.Run(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "setup failed:", err)
			os.Exit(1)
		}
		return
	case "uninstall":
		if err := wizard.New(nil).Uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall failed:", err)
			os.Exit(1)
		}
		return
	case "preview":
		// Render every tier against a sample payload so a user can see which
		// one their font actually supports. This is what the install wizard
		// drives -- there is no reliable way to probe glyph coverage, so the
		// user's eyes are the detector.
		preview(*cfgPath)
		return
	}

	if arg := flag.Arg(0); arg != "" {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", arg, usage)
		os.Exit(2)
	}
	// Rendering blocks on stdin, and Claude Code always pipes a payload. A
	// terminal on stdin means a human typed the bare command expecting output,
	// so show them what the commands are instead of hanging forever.
	if stdinIsTerminal() {
		fmt.Print(usage)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		// A broken config costs the user their whole theme, since Load returns
		// the defaults rather than a partial parse. Saying nothing at all made
		// that indistinguishable from "the colours changed" -- so the bar still
		// renders clean on stdout, and the reason goes to stderr, which Claude
		// Code does not paint. `doctor` remains the place that explains it.
		fmt.Fprintf(os.Stderr, "claude-statusline: %v\n", err)
		fmt.Fprintf(os.Stderr, "claude-statusline: using built-in defaults; run `claude-statusline doctor`\n")
		cfg = config.Default()
	}
	if *icons != "" {
		cfg.Display.Icons = *icons
	}
	if *colors != "" {
		cfg.Display.Color = *colors
	}
	if *caps != "" {
		cfg.Display.Caps = *caps
	}

	line := build(cfg, input.Parse(os.Stdin))
	os.Stdout.Write(append([]byte(line), '\n'))
}

// stdinIsTerminal reports whether stdin is a console rather than a pipe or a
// file. Works on Windows too: a console handle is not a character device in
// the POSIX sense, but Go still reports ModeCharDevice for it.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func build(cfg config.Config, p input.Payload) string {
	colorTier := render.DetectColorTier()
	if cfg.Display.Color != "" && cfg.Display.Color != "auto" {
		if t, ok := render.ParseColorTier(cfg.Display.Color); ok {
			colorTier = t
		}
	}
	iconTier, _ := render.ParseIconTier(cfg.Display.Icons)
	capTier, _ := render.ParseCapTier(cfg.Display.Caps)

	// 256-colour terminals get the theme_256 overrides folded in, because
	// nearest-match cannot invent colours the palette does not contain.
	cfg.Theme = cfg.Resolve(colorTier == render.Color256)

	b := segments.Builder{Cfg: cfg, Tier: iconTier, Color: colorTier}
	opts := render.DefaultOptions()
	opts.Color = colorTier
	opts.Caps = capTier
	if cfg.Display.Pad != "" {
		opts.Pad = cfg.Display.Pad
	}
	if cfg.Display.Sep != "" {
		opts.Sep = cfg.Display.Sep
	}
	return render.Line(b.Build(p), opts)
}

func samplePayload() input.Payload {
	cwd, _ := os.Getwd()
	return input.Payload{
		Model:  input.Model{ID: "claude-opus-5"},
		Effort: input.Effort{Level: "xhigh"},
		CWD:    cwd,
		Cost: input.Cost{
			TotalCostUSD: 13.26, TotalDurationMS: 1_466_000,
			LinesAdded: 121, LinesRemoved: 55,
		},
		Context: &input.Context{TotalInputTokens: 406_000, ContextWindowSize: 200_000},
		RateLimits: &input.RateLimits{
			// Reset stamps are relative to now so `preview` and the docs
			// screenshots exercise the countdown rather than silently
			// omitting it. At 25%/13% neither window is elevated, so the
			// default "warn" mode still hides it here -- which is the point:
			// the sample shows the quiet case, and states.png shows the loud
			// one.
			FiveHour: &input.Window{UsedPercentage: 25, ResetsAt: time.Now().Add(2 * time.Hour).Unix()},
			SevenDay: &input.Window{UsedPercentage: 13, ResetsAt: time.Now().Add(4 * 24 * time.Hour).Unix()},
		},
	}
}

func preview(cfgPath string) {
	cfg, _ := config.Load(cfgPath)
	p := samplePayload()
	type variant struct{ icons, caps, label string }
	for _, v := range []variant{
		{"nerd", "round", "nerd + rounded caps   (needs a Nerd Font)"},
		{"nerd", "none", "nerd, no caps"},
		{"unicode", "block", "unicode + block caps  (most fonts)"},
		{"ascii", "none", "ascii labels          (works everywhere)"},
	} {
		c := cfg
		c.Display.Icons, c.Display.Caps = v.icons, v.caps
		fmt.Printf("%s\n  %s\n\n", v.label, build(c, p))
	}
}

// checkWiring reports on Claude Code's statusLine setting. "Installed it but
// nothing shows up" is almost always this: the key is missing, or it points at
// a path that has since moved or been deleted. Returns false if the status line
// cannot be running.
func checkWiring() bool {
	path, err := claudecode.SettingsPath()
	if err != nil {
		return true // can't tell; don't cry wolf
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("claude code : no %s\n", path)
		fmt.Printf("              run `claude-statusline init` to wire it up\n")
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		// Claude Code tolerates comments in settings.json; we only parse it to
		// look, so a parse failure is not itself a fault.
		fmt.Printf("claude code : %s isn't strict JSON, can't check it\n", path)
		return true
	}
	cmd := claudecode.StatusLineCommand(root)
	if cmd == "" {
		fmt.Printf("claude code : no statusLine configured in %s\n", path)
		fmt.Printf("              run `claude-statusline init` to wire it up\n")
		return false
	}
	// Live, not Stat: a status line is often a script run through an
	// interpreter, where the executable is a name on PATH rather than a file.
	// Stat called every one of those missing and told the user to repoint a
	// wiring that worked.
	if !claudecode.Live(cmd) {
		fmt.Printf("claude code : statusLine points at a missing file --\n")
		fmt.Printf("              %s\n", cmd)
		fmt.Printf("              re-run `claude-statusline init` to repoint it\n")
		return false
	}
	fmt.Printf("claude code : wired -> %s\n", cmd)
	if self, err := os.Executable(); err == nil && !claudecode.SameCommand(cmd, self) {
		fmt.Printf("              note: that is not this binary (%s)\n", self)
	}
	return true
}

func doctor(cfgPath string) int {
	cfg, err := config.Load(cfgPath)
	fmt.Printf("config      : %s\n", config.Path())
	if err != nil {
		fmt.Printf("  ERROR     : %v\n", err)
		return 1
	}
	fmt.Printf("colour tier : detected=%v configured=%q\n", render.DetectColorTier(), cfg.Display.Color)
	if render.NoColorSet() {
		fmt.Println("              note: NO_COLOR is set but deliberately ignored --")
		fmt.Println("              Claude Code sets it for subprocesses. Use color=\"none\".")
	}
	fmt.Printf("icon tier   : %q\n", cfg.Display.Icons)
	fmt.Printf("git cache   : %v\n", cfg.Display.CacheTTL())

	bad := 0
	if !checkWiring() {
		bad++
	}

	// Contrast audit: white-on-pill below AAA is hard to read at terminal
	// font sizes even though it passes WCAG AA, which assumes larger text.
	fmt.Println("\ncontrast of white text on each pill (AAA = 7.0):")
	for _, e := range []struct {
		name string
		hex  config.ColorValue
	}{
		{"model/low", cfg.Theme.EffortLow}, {"model/medium", cfg.Theme.EffortMedium},
		{"model/high", cfg.Theme.EffortHigh}, {"model/xhigh", cfg.Theme.EffortXHigh},
		{"model/max", cfg.Theme.EffortMax},
		{"context/ok", cfg.Theme.CtxOK}, {"context/warn", cfg.Theme.CtxWarn},
		{"context/crit", cfg.Theme.CtxCrit},
		{"cost", cfg.Theme.Cost}, {"limits", cfg.Theme.Limits},
		{"dir", cfg.Theme.Dir}, {"git", cfg.Theme.Git},
	} {
		c, err := render.ParseColor(string(e.hex))
		if err != nil {
			fmt.Printf("  %-14s %s  INVALID: %v\n", e.name, e.hex, err)
			bad++
			continue
		}
		r := c.ContrastWithWhite()
		flag := ""
		if r < 4.5 {
			flag, bad = "  <-- FAILS WCAG AA", bad+1
		} else if r < 7.0 {
			flag = "  (AA, below AAA)"
		}
		fmt.Printf("  %-14s %-8s %5.2f%s\n", e.name, e.hex, r, flag)
	}
	if bad > 0 {
		return 1
	}
	return 0
}
