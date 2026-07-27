package wizard

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PouriahLabs/claude-statusline/internal/claudecode"
	"github.com/PouriahLabs/claude-statusline/internal/config"
)

// newTestWizard drives the wizard off a canned answer script and captures what
// it prints, so the tests can assert on both the choice and the preview rows.
func newTestWizard(answers string) (*Wizard, *strings.Builder) {
	var out strings.Builder
	w := &Wizard{
		In:  bufio.NewReader(strings.NewReader(answers)),
		Out: &out,
		Render: func(c config.Config) string {
			return "icons=" + c.Display.Icons + " caps=" + c.Display.Caps
		},
	}
	return w, &out
}

func TestCreateSettingsWritesUsableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json") // dir absent
	w, out := newTestWizard("")

	w.createSettings(path, "/opt/bin/claude-statusline", "SNIPPET")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no settings written: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("wrote invalid JSON (%v):\n%s", err, b)
	}
	if got := claudecode.StatusLineCommand(root); got != "/opt/bin/claude-statusline" {
		t.Errorf("command = %q, want /opt/bin/claude-statusline", got)
	}
	if strings.Contains(out.String(), "SNIPPET") {
		t.Errorf("told the user to do it by hand after succeeding:\n%s", out.String())
	}
}

// exe comes off the filesystem, so a quote or backslash in it must not be able
// to produce a settings.json Claude Code then refuses to parse.
func TestCreateSettingsEscapesAwkwardPaths(t *testing.T) {
	for _, exe := range []string{
		`C:\Users\pouria\bin\claude-statusline.exe`,
		`/home/a "quoted" dir/claude-statusline`,
		"/home/tab\there/claude-statusline",
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		w, _ := newTestWizard("")

		w.createSettings(path, exe, "SNIPPET")

		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("no settings written for %q: %v", exe, err)
		}
		var root map[string]any
		if err := json.Unmarshal(b, &root); err != nil {
			t.Fatalf("invalid JSON for exe %q (%v):\n%s", exe, err, b)
		}
		// What is written is the shell-safe rendering, so check it resolves
		// back to the binary we meant rather than matching it byte for byte.
		// Separator style may differ: on Windows the command is deliberately
		// written with forward slashes so a shell cannot eat the backslashes.
		got := claudecode.StatusLineCommand(root)
		resolved := claudecode.ResolveCommand(got)
		norm := func(s string) string { return strings.ReplaceAll(s, `\`, "/") }
		if norm(resolved) != norm(exe) {
			t.Errorf("command %q resolves to %q, want %q", got, resolved, exe)
		}
	}
}

// A file appearing between the existence check and the write must survive --
// that race is someone else's settings.json.
func TestCreateSettingsRefusesToClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	const existing = `{"statusLine":{"type":"command","command":"/usr/bin/other"}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	w, out := newTestWizard("")

	w.createSettings(path, "/opt/bin/claude-statusline", "SNIPPET")

	b, _ := os.ReadFile(path)
	if string(b) != existing {
		t.Errorf("clobbered an existing file:\n%s", b)
	}
	if !strings.Contains(out.String(), "SNIPPET") {
		t.Errorf("failed without telling the user how to wire it by hand:\n%s", out.String())
	}
}

func TestWantsTextLabels(t *testing.T) {
	for _, tc := range []struct {
		name, answers string
		want          bool
	}{
		{"text", "text\n", true},
		{"icons", "icons\n", false},
		{"default is icons", "\n", false},
		{"case insensitive", "TEXT\n", true},
		{"rejects then accepts", "words\ntext\n", true},
		{"eof takes the default", "te", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newTestWizard(tc.answers)
			if got := w.wantsTextLabels(config.Default()); got != tc.want {
				t.Errorf("wantsTextLabels() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The whole point of the step is that choosing text skips glyph detection --
// including the font install offer, which is a download prompt aimed at a
// problem the user just said they don't have.
func TestTextLabelsSkipTheGlyphChain(t *testing.T) {
	w, out := newTestWizard("text\n")
	cfg := config.Default()

	if !w.wantsTextLabels(cfg) {
		t.Fatal("answering text did not select text labels")
	}
	for _, unwanted := range []string{"Nerd Font", "robot", "Do you see"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("text path reached %q:\n%s", unwanted, out.String())
		}
	}
}

// Both samples must be drawable, or the step asks the user to compare a row of
// tofu against a row of words.
func TestWantsTextLabelsPreviewsBothTiers(t *testing.T) {
	w, out := newTestWizard("\n")
	w.wantsTextLabels(config.Default())

	for _, want := range []string{"icons=unicode caps=block", "icons=ascii caps=none"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from the samples:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "icons=nerd") {
		t.Errorf("previewed the nerd tier before detecting it:\n%s", out.String())
	}
}

// The samples are rendered from the caller's config, so a theme loaded from
// disk is not silently swapped for a zero value in the preview.
func TestWantsTextLabelsRendersFromGivenConfig(t *testing.T) {
	var out strings.Builder
	w := &Wizard{
		In:     bufio.NewReader(strings.NewReader("\n")),
		Out:    &out,
		Render: func(c config.Config) string { return "dir=" + string(c.Theme.Dir) },
	}
	cfg := config.Default()
	cfg.Theme.Dir = "#abcdef"

	w.wantsTextLabels(cfg)

	if !strings.Contains(out.String(), "#abcdef") {
		t.Errorf("samples ignored the passed config:\n%s", out.String())
	}
}

func TestCapsForHidesNerdCapsFromPlainFonts(t *testing.T) {
	for _, tc := range []struct {
		icons string
		want  int
	}{
		{"nerd", 4},
		{"unicode", 2},
		{"ascii", 2},
		{"NERD", 4},
	} {
		if got := len(capsFor(tc.icons)); got != tc.want {
			t.Errorf("capsFor(%q) offered %d choices, want %d", tc.icons, got, tc.want)
		}
	}
}

// The install-a-font path leaves the user on unicode icons until they switch
// their terminal over. Previewing powerline caps there prints tofu and asks
// which row looks right -- see the wizard's own "using unicode icons" note.
func TestChooseCapsSkipsPowerlinePreviewsOnUnicode(t *testing.T) {
	w, out := newTestWizard("\n")
	cfg := config.Default()
	cfg.Display.Icons, cfg.Display.Caps = "unicode", "block"

	got := w.chooseCaps(cfg)

	if strings.Contains(out.String(), "caps=round") || strings.Contains(out.String(), "caps=arrow") {
		t.Errorf("previewed a Nerd Font cap on a unicode tier:\n%s", out.String())
	}
	if got.Display.Caps != "block" {
		t.Errorf("caps = %q, want the block default", got.Display.Caps)
	}
}

func TestChooseCapsOffersEverythingOnNerd(t *testing.T) {
	w, out := newTestWizard("arrow\n")
	cfg := config.Default()
	cfg.Display.Icons = "nerd"

	got := w.chooseCaps(cfg)

	for _, want := range []string{"caps=round", "caps=arrow", "caps=block", "caps=none"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from the previews:\n%s", want, out.String())
		}
	}
	if got.Display.Caps != "arrow" {
		t.Errorf("caps = %q, want arrow", got.Display.Caps)
	}
}

// A cap the current tier cannot draw must not survive as the default, or the
// prompt offers block/none while "(default round)" quietly keeps round.
func TestChooseCapsRewritesUndrawableDefault(t *testing.T) {
	w, out := newTestWizard("\n")
	cfg := config.Default() // caps defaults to "round"
	cfg.Display.Icons = "unicode"

	got := w.chooseCaps(cfg)

	if got.Display.Caps != "block" {
		t.Errorf("caps = %q, want block", got.Display.Caps)
	}
	if !strings.Contains(out.String(), "(default block)") {
		t.Errorf("prompt advertised an undrawable default:\n%s", out.String())
	}
}

// Answering with a cap that is offered on another tier should not be silently
// swallowed into the default.
func TestChooseCapsRejectsUnofferedAnswer(t *testing.T) {
	w, out := newTestWizard("round\nnone\n")
	cfg := config.Default()
	cfg.Display.Icons, cfg.Display.Caps = "unicode", "block"

	got := w.chooseCaps(cfg)

	if !strings.Contains(out.String(), "not one of those") {
		t.Errorf("accepted an unoffered answer without comment:\n%s", out.String())
	}
	if got.Display.Caps != "none" {
		t.Errorf("caps = %q, want none", got.Display.Caps)
	}
}

// EOF mid-prompt (piped stdin that ran dry) must not spin.
func TestChooseCapsTerminatesOnEOF(t *testing.T) {
	w, _ := newTestWizard("round")
	cfg := config.Default()
	cfg.Display.Icons, cfg.Display.Caps = "unicode", "block"

	if got := w.chooseCaps(cfg); got.Display.Caps != "block" {
		t.Errorf("caps = %q, want the block default", got.Display.Caps)
	}
}

func TestChooseCapsSkippedForASCII(t *testing.T) {
	w, out := newTestWizard("")
	cfg := config.Default()
	cfg.Display.Icons, cfg.Display.Caps = "ascii", "none"

	if got := w.chooseCaps(cfg); got.Display.Caps != "none" {
		t.Errorf("caps = %q, want none", got.Display.Caps)
	}
	if out.String() != "" {
		t.Errorf("asked about pill shape on the ascii tier:\n%s", out.String())
	}
}

// writeSettings builds a fake ~/.claude/settings.json wired to cmd and points
// the wizard's home at it.
func writeSettings(t *testing.T, cmd string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"statusLine":{"type":"command","command":` + strconv.Quote(cmd) + `}}`
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	return path
}

// The regression that produced an empty status line after a successful install:
// a statusLine whose binary is gone is broken, not somebody else's, and
// defaulting to "leave it alone" left Claude Code with nothing to run.
func TestWiringRepointsADeadStatusLineByDefault(t *testing.T) {
	path := writeSettings(t, filepath.Join(t.TempDir(), "uninstalled-tool"))
	w, out := newTestWizard("\n") // just press Enter

	w.offerClaudeWiring()

	if !strings.Contains(out.String(), "points at something that") {
		t.Errorf("did not report the dead pointer:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("repointing a dead statusLine must default to yes:\n%s", out.String())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "claude-statusline") && !strings.Contains(out.String(), "done") {
		t.Errorf("pressing Enter left the broken pointer in place:\n%s\n%s", out.String(), b)
	}
}

// A statusLine that still resolves is another tool doing its job, and keeps the
// cautious default.
func TestWiringKeepsALiveStatusLineByDefault(t *testing.T) {
	other := filepath.Join(t.TempDir(), "other-statusline")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeSettings(t, other)
	before, _ := os.ReadFile(path)
	w, out := newTestWizard("\n")

	w.offerClaudeWiring()

	if !strings.Contains(out.String(), "[y/N]") {
		t.Errorf("replacing a working statusLine must default to no:\n%s", out.String())
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("pressing Enter modified a working statusLine:\n%s", after)
	}
}
