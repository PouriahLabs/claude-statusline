package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A missing config must not be an error: the tool has to render with zero
// setup, on a machine where the user has never run `init`.
func TestLoadMissingFileUsesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file returned error: %v", err)
	}
	if c.Theme.Limits != Default().Theme.Limits {
		t.Errorf("defaults not applied: %+v", c.Theme)
	}
}

// The user's file is layered OVER the defaults, so a partial config only
// overrides what it names.
func TestLoadIsPartialOverlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[theme]
limits = "#111111"

[display]
icons = "ascii"
`), 0o644)

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Theme.Limits != "#111111" {
		t.Errorf("override not applied: %q", c.Theme.Limits)
	}
	if c.Display.Icons != "ascii" {
		t.Errorf("display override not applied: %q", c.Display.Icons)
	}
	// Untouched keys must survive.
	d := Default()
	if c.Theme.Dir != d.Theme.Dir {
		t.Errorf("unspecified theme key was clobbered: %q want %q", c.Theme.Dir, string(d.Theme.Dir))
	}
	if len(c.Order) != len(d.Order) {
		t.Errorf("order was clobbered: %v", c.Order)
	}
	if len(c.Windows) != len(d.Windows) {
		t.Errorf("context_window defaults were clobbered: %v", c.Windows)
	}
}

// Malformed TOML falls back to defaults AND reports the error, so `doctor`
// can surface it while the status line still renders.
func TestLoadMalformedReturnsDefaultsAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(path, []byte("this is not = = toml"), 0o644)

	c, err := Load(path)
	if err == nil {
		t.Error("malformed config should report an error")
	}
	if c.Theme.Limits != Default().Theme.Limits {
		t.Error("malformed config should fall back to defaults")
	}
}

func TestCacheTTL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"2s", 2 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{"0", 0},
		{"", 2 * time.Second},        // unset means the default
		{"garbage", 2 * time.Second}, // unparseable means the default
	} {
		if got := (Display{GitCache: tc.in}).CacheTTL(); got != tc.want {
			t.Errorf("CacheTTL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDirRespectsEnvOverride(t *testing.T) {
	t.Setenv("CLAUDE_STATUSLINE_CONFIG_DIR", "/custom/path")
	if got := Dir(); got != "/custom/path" {
		t.Errorf("Dir() = %q, want /custom/path", got)
	}
}

// Every default colour must parse, or the tool ships with a broken theme.
func TestDefaultThemeParses(t *testing.T) {
	d := Default()
	for name, hex := range map[string]string{
		"effort_low": string(d.Theme.EffortLow), "effort_medium": string(d.Theme.EffortMedium),
		"effort_high": string(d.Theme.EffortHigh), "effort_xhigh": string(d.Theme.EffortXHigh),
		"effort_max": string(d.Theme.EffortMax), "model_default": string(d.Theme.ModelDefault),
		"ctx_ok": string(d.Theme.CtxOK), "ctx_warn": string(d.Theme.CtxWarn), "ctx_crit": string(d.Theme.CtxCrit),
		"cost": string(d.Theme.Cost), "limits": string(d.Theme.Limits),
		"limits_warn_fg": string(d.Theme.LimitsWarn), "limits_crit_fg": string(d.Theme.LimitsCrit),
		"dir": string(d.Theme.Dir), "git": string(d.Theme.Git), "text": string(d.Theme.Text),
		"diff_add": string(d.Theme.DiffAdd), "diff_del": string(d.Theme.DiffDel),
	} {
		if hex == "" {
			t.Errorf("default theme key %q is empty", name)
		}
	}
}
