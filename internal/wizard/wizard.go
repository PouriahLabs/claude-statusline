// Package wizard is the interactive first-run setup.
//
// Its central job is deciding which icon tier the user's font can actually
// draw. No portable API reports glyph coverage, so the wizard prints the
// candidates and asks. That is the same approach powerlevel10k takes, and it
// is the only one that does not guess wrong.
package wizard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/PouriahLabs/claude-statusline/internal/claudecode"
	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/fontinstall"
	"github.com/PouriahLabs/claude-statusline/internal/terminal"
)

type Wizard struct {
	In     *bufio.Reader
	Out    io.Writer
	Render func(cfg config.Config) string // renders the sample bar
}

func New(render func(config.Config) string) *Wizard {
	return &Wizard{In: bufio.NewReader(os.Stdin), Out: os.Stdout, Render: render}
}

func (w *Wizard) printf(f string, a ...any) { fmt.Fprintf(w.Out, f, a...) }

func (w *Wizard) ask(q string, def bool) bool {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		w.printf("%s %s ", q, hint)
		line, err := w.In.ReadString('\n')
		if err != nil {
			return def
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

// Run executes the full flow and returns the config to save.
func (w *Wizard) Run(cfg config.Config) (config.Config, error) {
	w.printf("\nclaude-statusline setup\n=======================\n\n")

	if w.wantsTextLabels(cfg) {
		cfg.Display.Icons, cfg.Display.Caps = "ascii", "none"
	} else {
		cfg = w.chooseIcons(cfg)
	}
	cfg = w.chooseCaps(cfg)

	w.printf("\nYour bar:\n\n  %s\n\n", w.Render(cfg))

	if err := w.save(cfg); err != nil {
		return cfg, err
	}
	w.offerClaudeWiring()
	return cfg, nil
}

// choice prompts until the answer is one of opts. Empty input takes def, and
// so does a read error -- a piped stdin that runs dry must not spin.
func (w *Wizard) choice(question string, opts []string, def string) string {
	prompt := fmt.Sprintf("%s [%s] (default %s) ", question, strings.Join(opts, "/"), def)
	for {
		w.printf("%s", prompt)
		line, err := w.In.ReadString('\n')
		s := strings.ToLower(strings.TrimSpace(line))
		if s == "" {
			return def
		}
		for _, o := range opts {
			if s == o {
				return o
			}
		}
		if err != nil {
			return def
		}
		w.printf("  not one of those.\n")
	}
}

// wantsTextLabels is the first fork, before any glyph detection.
//
// Icons were previously reachable-only: the ascii tier sat at the bottom of the
// detection chain, so you got there by answering "no" three times and never by
// choosing it. Preferring words to symbols is a taste, not a failure, and the
// people who hold it should not have to sit through a font install offer to say
// so. Answering "text" skips the whole chain.
func (w *Wizard) wantsTextLabels(cfg config.Config) bool {
	w.printf("Icons or text?\n--------------\n\n")
	w.printf("Icons need a font that has the glyphs. Text labels need nothing,\n")
	w.printf("and read the same in every terminal.\n\n")

	// The icon sample uses the unicode tier, not nerd: it is the one most fonts
	// can already draw, so it is an honest picture of what "icons" gets you
	// before any font install. If the nerd glyphs turn out to work, the next
	// step upgrades to them.
	icons, text := cfg, cfg
	icons.Display.Icons, icons.Display.Caps = "unicode", "block"
	text.Display.Icons, text.Display.Caps = "ascii", "none"
	w.printf("  %-6s %s\n", "icons", w.Render(icons))
	w.printf("  %-6s %s\n", "text", w.Render(text))

	return w.choice("\nWhich do you want?", []string{"icons", "text"}, "icons") == "text"
}

func (w *Wizard) chooseIcons(cfg config.Config) config.Config {
	// Deliberately not "step N of M": the terminal-patch step only happens on
	// the font-install path, so a fixed total would promise a step that never
	// arrives for users whose font already works.
	w.printf("Icons\n-----\n\n")
	w.printf("There is no way for a program to detect which glyphs your font has,\n")
	w.printf("so you get to be the detector.\n\n")

	nerd := cfg
	nerd.Display.Icons, nerd.Display.Caps = "nerd", "round"
	w.printf("  %s\n\n", w.Render(nerd))

	if w.ask("Do you see a robot, a chip, a clock and a folder above, with rounded pill ends?", true) {
		cfg.Display.Icons = "nerd"
		return cfg
	}

	if w.ask("\nInstall a Nerd Font now? (per-user, no admin needed, ~10MB)", true) {
		if fam, ok := w.installFont(); ok {
			w.offerTerminalPatch(fam)
			w.printf("\nOnce your terminal is using %q, open a NEW tab and re-run:\n", fam)
			w.printf("  claude-statusline init\n\n")
			w.printf("Using unicode icons until then.\n")
			cfg.Display.Icons, cfg.Display.Caps = "unicode", "block"
			return cfg
		}
	}

	uni := cfg
	uni.Display.Icons, uni.Display.Caps = "unicode", "block"
	w.printf("\nHow about these?\n\n  %s\n\n", w.Render(uni))
	if w.ask("Do those symbols all render (no boxes or question marks)?", true) {
		cfg.Display.Icons, cfg.Display.Caps = "unicode", "block"
		return cfg
	}

	w.printf("\nFalling back to plain text labels, which work in every terminal.\n")
	cfg.Display.Icons, cfg.Display.Caps = "ascii", "none"
	return cfg
}

type capChoice struct{ name, label string }

var (
	nerdCaps  = []capChoice{{"round", "rounded"}, {"arrow", "arrows"}, {"block", "blocks"}, {"none", "none"}}
	plainCaps = []capChoice{{"block", "blocks"}, {"none", "none"}}
)

// capsFor returns the cap styles the given icon tier can actually draw.
// round and arrow are powerline glyphs from the same private-use range as the
// nerd icons, so offering them to a font without them shows the user four rows
// of tofu and asks which one looks right -- the exact question they just
// answered "no" to.
func capsFor(iconTier string) []capChoice {
	if strings.ToLower(iconTier) == "nerd" {
		return nerdCaps
	}
	return plainCaps
}

func (w *Wizard) chooseCaps(cfg config.Config) config.Config {
	if cfg.Display.Icons == "ascii" {
		return cfg
	}
	choices := capsFor(cfg.Display.Icons)

	// The default carried in from chooseIcons is always drawable, but a config
	// loaded from disk may name a cap this tier cannot render.
	if !hasCap(choices, cfg.Display.Caps) {
		cfg.Display.Caps = choices[0].name
	}

	w.printf("\nPill shape\n----------\n\n")
	for _, c := range choices {
		v := cfg
		v.Display.Caps = c.name
		w.printf("  %-8s %s\n", c.label, w.Render(v))
	}
	if len(choices) < len(nerdCaps) {
		w.printf("\n(Rounded and arrow caps are Nerd Font glyphs -- they'd draw as boxes\n")
		w.printf("in this font. Re-run `claude-statusline init` once the font is active\n")
		w.printf("to pick one.)\n")
	}

	names := make([]string, len(choices))
	for i, c := range choices {
		names[i] = c.name
	}
	cfg.Display.Caps = w.choice("\nWhich looks right?", names, cfg.Display.Caps)
	return cfg
}

func hasCap(choices []capChoice, name string) bool {
	for _, c := range choices {
		if c.name == name {
			return true
		}
	}
	return false
}

func (w *Wizard) installFont() (string, bool) {
	f := fontinstall.Catalog[0]
	w.printf("\nAvailable fonts:\n")
	for i, c := range fontinstall.Catalog {
		w.printf("  %d) %s\n", i+1, c.Label)
	}
	w.printf("Choice [1-%d] (default 1) ", len(fontinstall.Catalog))
	line, _ := w.In.ReadString('\n')
	if s := strings.TrimSpace(line); s != "" {
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n >= 1 && n <= len(fontinstall.Catalog) {
			f = fontinstall.Catalog[n-1]
		}
	}
	w.printf("\n")
	if _, err := fontinstall.Install(f, func(m string) { w.printf("  %s\n", m) }); err != nil {
		w.printf("\n  font install failed: %v\n", err)
		return "", false
	}
	w.printf("\n  installed. Family name: %q\n", f.Family)
	return f.Family, true
}

func (w *Wizard) offerTerminalPatch(family string) {
	found := terminal.Detect()
	if len(found) == 0 {
		w.printf("\nNo known terminal config found. Set your font to %q manually.\n", family)
		return
	}
	w.printf("\nPoint your terminal at the font\n-------------------------------\n\n")
	for _, t := range found {
		ok, why := t.CanPatch()
		if !ok {
			w.printf("  %s -- can't edit automatically (%s)\n", t.Name, why)
			w.printf("      add this yourself:\n        %s\n\n",
				strings.ReplaceAll(t.Instructions(family), "\n", "\n        "))
			continue
		}
		if !w.ask(fmt.Sprintf("  Patch %s (%s)? A backup will be written.", t.Name, t.Path), true) {
			w.printf("      skipped. Add this yourself:\n        %s\n\n", t.Instructions(family))
			continue
		}
		bak, err := t.Backup()
		if err != nil {
			w.printf("      backup failed, not touching it: %v\n\n", err)
			continue
		}
		if err := t.Patch(family); err != nil {
			w.printf("      patch failed: %v (backup at %s)\n\n", err, bak)
			continue
		}
		w.printf("      done. Backup: %s\n\n", bak)
	}
}

func (w *Wizard) save(cfg config.Config) error {
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		return err
	}
	path := config.Path()
	if _, err := os.Stat(path); err == nil {
		if !w.ask(fmt.Sprintf("Overwrite existing config at %s?", path), true) {
			w.printf("Left it alone.\n")
			return nil
		}
		src, _ := os.ReadFile(path)
		_ = os.WriteFile(path+".bak-"+time.Now().Format("20060102-150405"), src, 0o644)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# claude-statusline configuration\n")
	fmt.Fprintf(f, "# Docs: https://github.com/PouriahLabs/claude-statusline\n#\n")
	fmt.Fprintf(f, "# Only the settings you chose are written here. Everything else follows\n")
	fmt.Fprintf(f, "# the built-in defaults and will track any future changes to them.\n")
	fmt.Fprintf(f, "# See config.example.toml for the full list of keys.\n\n")
	// Just the deltas: a full dump would pin every default at install time, and
	// a pinned key wins over the shipped value forever. See config.Minimal.
	if err := toml.NewEncoder(f).Encode(config.Minimal(cfg, "display.icons", "display.caps")); err != nil {
		return err
	}
	w.printf("Wrote %s\n", path)
	return nil
}

var jsonComment = regexp.MustCompile(`(?m)^\s*//`)

// offerClaudeWiring registers the binary as Claude Code's statusLine command.
func (w *Wizard) offerClaudeWiring() {
	path, err := claudecode.SettingsPath()
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// One rendering of the command for every path below -- the file we write and
	// the snippet we print must not disagree about what actually runs.
	cmd := claudecode.CommandFor(exe)
	cmdJSON, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	snippet := fmt.Sprintf(`"statusLine": { "type": "command", "command": %s }`, cmdJSON)

	w.printf("\nFinally: tell Claude Code to use it.\n\n")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No file means nothing to overwrite and nothing to preserve, so there
		// is no decision to hand back: printing JSON for someone to paste is
		// asking them to do by hand the one thing they ran init for. Every
		// other branch below has something to lose and still asks.
		w.createSettings(path, exe, snippet)
		return
	}
	if err != nil {
		w.printf("  can't read %s: %v\n\n  Add this yourself:\n\n    %s\n\n", path, err, snippet)
		return
	}
	if jsonComment.Match(b) {
		w.printf("  %s has comments, so I won't rewrite it. Add:\n\n    %s\n\n", path, snippet)
		return
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		w.printf("  %s isn't strict JSON (%v). Add:\n\n    %s\n\n", path, err, snippet)
		return
	}
	// Replacing someone else's status line is not the same act as adding one to
	// a file that has none, so it does not get the same prompt. Name what is
	// there, and make the destructive answer the one you have to type: an
	// unnoticed Enter must not silently unwire another tool.
	prompt, def := fmt.Sprintf("  Add statusLine to %s? A backup will be written.", path), true
	if cur := claudecode.StatusLineCommand(root); cur != "" {
		if claudecode.SameCommand(cur, exe) {
			w.printf("  Already wired to this binary -- nothing to change.\n")
			return
		}
		if !claudecode.Live(cur) {
			// A command whose binary is gone is not another tool's status line,
			// it is a broken one -- Claude Code runs it, gets nothing back, and
			// draws no bar at all. Defaulting to "leave it alone" here left the
			// user staring at an empty status line after an install that had
			// apparently succeeded, which is how this case was found.
			w.printf("  %s has a statusLine, but it points at something that\n", path)
			w.printf("  is not there:\n\n    %s\n\n", cur)
			w.printf("  Claude Code shows no status line at all while that is the case.\n")
			prompt = "  Repoint it at claude-statusline? A backup will be written."
			def = true
		} else {
			w.printf("  %s already has a statusLine:\n\n    %s\n\n", path, cur)
			prompt = "  Replace it with claude-statusline? A backup will be written."
			def = false
		}
	}
	if !w.ask(prompt, def) {
		w.printf("  Left it alone. To wire it up yourself:\n\n    %s\n\n", snippet)
		return
	}
	bak := path + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(bak, b, 0o644); err != nil {
		// The terminal patcher refuses to edit without a backup; the file that
		// may hold another tool's config has more to lose, not less.
		w.printf("  backup failed, not touching it: %v\n", err)
		return
	}
	root["statusLine"] = map[string]any{"type": "command", "command": cmd}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		w.printf("  write failed: %v (backup at %s)\n", err, bak)
		return
	}
	w.printf("  done (backup: %s). Restart Claude Code to see it.\n", bak)
}

// createSettings writes a fresh settings.json wiring up this binary. Only
// called when the file does not exist, so it never needs a backup or a prompt.
func (w *Wizard) createSettings(path, exe, snippet string) {
	fallback := func(format string, a ...any) {
		w.printf(format, a...)
		w.printf("\n  Add this yourself:\n\n    %s\n\n", snippet)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fallback("  couldn't create %s: %v\n", filepath.Dir(path), err)
		return
	}
	// Marshal rather than format a string: exe is a path from the filesystem,
	// and a backslash or a quote in it must not produce a settings.json that
	// Claude Code then refuses to parse.
	out, err := json.MarshalIndent(map[string]any{
		"statusLine": map[string]any{"type": "command", "command": claudecode.CommandFor(exe)},
	}, "", "  ")
	if err != nil {
		fallback("  couldn't build the settings: %v\n", err)
		return
	}
	// O_EXCL so a file that appears between the read above and this write is
	// left alone rather than silently replaced.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fallback("  couldn't write %s: %v\n", path, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(out, '\n')); err != nil {
		fallback("  couldn't write %s: %v\n", path, err)
		return
	}
	w.printf("  wrote %s -- Claude Code is wired up.\n", path)
	w.printf("  Restart Claude Code to see it.\n")
}
