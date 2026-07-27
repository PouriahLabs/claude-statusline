package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

// loadVia encodes v as the config file and loads it back.
func loadVia(t *testing.T, v any) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.NewEncoder(f).Encode(v); err != nil {
		f.Close()
		t.Fatalf("encode failed: %v", err)
	}
	f.Close()
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	return got
}

var minimalCases = []struct {
	name   string
	mutate func(*Config)
}{
	{"untouched defaults", func(*Config) {}},
	{"icon tier", func(c *Config) { c.Display.Icons, c.Display.Caps = "ascii", "none" }},
	{"nested theme colour", func(c *Config) { c.Theme.Dir = "#ff0000" }},
	{"threshold", func(c *Config) { c.Limits.Warn = 42 }},
	{"reordered segments", func(c *Config) { c.Order = []string{"git", "model"} }},
	{"model names", func(c *Config) { c.ModelNames = map[string]string{"^x": "X"} }},
	{"context windows", func(c *Config) { c.Windows = []ContextWindow{{Match: "^y", Size: 42}} }},
	{"empty order", func(c *Config) { c.Order = nil }},
	{"several at once", func(c *Config) {
		c.Display.Icons, c.Theme.Cost, c.Context.Crit = "unicode", "#123456", 91
	}},
}

// The guarantee that matters: dropping the untouched keys must not change what
// loads. Compared against a full-struct dump -- what the wizard used to write --
// rather than against the in-memory config, because Load deliberately layers
// over the defaults and a couple of shapes cannot survive that trip either way
// (see TestLoadMergesMapsRatherThanReplacing). Writing less must be no worse.
func TestMinimalLoadsTheSameAsAFullDump(t *testing.T) {
	for _, tc := range minimalCases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mutate(&c)

			full := loadVia(t, c)
			min := loadVia(t, Minimal(c))

			if !reflect.DeepEqual(full, min) {
				t.Errorf("minimal config loads differently than a full dump:\n min  %+v\n full %+v", min, full)
			}
		})
	}
}

// And for everything the layering model can express, the settings survive
// intact. The two shapes excluded here are the ones a full dump cannot round
// trip either.
func TestMinimalRoundTrips(t *testing.T) {
	for _, tc := range minimalCases {
		if tc.name == "model names" || tc.name == "empty order" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			want := Default()
			tc.mutate(&want)

			if got := loadVia(t, Minimal(want)); !reflect.DeepEqual(got, want) {
				t.Errorf("round trip lost settings:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

// Pre-existing and older than Minimal: Load decodes over a pre-populated
// Config, and TOML merges maps rather than replacing them. So a user can add a
// model name but cannot remove a shipped one, and an empty order reverts to the
// default rather than clearing the bar. Recorded here so the behaviour is a
// decision rather than a surprise -- changing it means changing Load.
func TestLoadMergesMapsRatherThanReplacing(t *testing.T) {
	c := Default()
	c.ModelNames = map[string]string{"^x": "X"}
	c.Order = nil

	got := loadVia(t, c)

	if len(got.ModelNames) != len(Default().ModelNames)+1 {
		t.Errorf("ModelNames has %d entries, expected the defaults plus one",
			len(got.ModelNames))
	}
	if got.ModelNames["^x"] != "X" {
		t.Errorf("the added name did not survive: %v", got.ModelNames)
	}
	if !reflect.DeepEqual(got.Order, Default().Order) {
		t.Errorf("Order = %v, expected the default to survive an empty list", got.Order)
	}
}

// The point of the exercise: a config that changes nothing writes nothing, so
// every default stays live and a later improvement reaches the user.
func TestMinimalOmitsUntouchedDefaults(t *testing.T) {
	m := Minimal(Default())
	if len(m) != 0 {
		t.Errorf("wrote %d keys for an unmodified config, want 0: %v", len(m), m)
	}
}

func TestMinimalWritesOnlyWhatChanged(t *testing.T) {
	c := Default()
	c.Display.Icons = "ascii"
	c.Theme.Dir = "#ff0000"

	m := Minimal(c)

	display, ok := m["display"].(map[string]any)
	if !ok {
		t.Fatalf("no display table: %v", m)
	}
	if len(display) != 1 || display["icons"] != "ascii" {
		t.Errorf("display = %v, want only icons", display)
	}
	if theme, _ := m["theme"].(map[string]any); len(theme) != 1 || theme["dir"] != ColorValue("#ff0000") {
		t.Errorf("theme = %v, want only dir", m["theme"])
	}
	for _, absent := range []string{"context", "limits", "order", "model_names", "context_window"} {
		if _, ok := m[absent]; ok {
			t.Errorf("wrote %q, which the user never touched", absent)
		}
	}
}

// An icon tier is a claim about the user's font. A future default cannot know
// better, so an explicit answer is pinned even when it matches today's value.
func TestMinimalKeepPinsMatchingDefaults(t *testing.T) {
	c := Default() // icons/caps already equal the defaults

	m := Minimal(c, "display.icons", "display.caps")

	display, ok := m["display"].(map[string]any)
	if !ok {
		t.Fatalf("keep path did not create the table: %v", m)
	}
	if display["icons"] != c.Display.Icons || display["caps"] != c.Display.Caps {
		t.Errorf("display = %v, want the pinned icons and caps", display)
	}
	if len(m) != 1 {
		t.Errorf("keep pulled in more than it was asked for: %v", m)
	}
}

func TestMinimalIgnoresUnknownKeepPath(t *testing.T) {
	m := Minimal(Default(), "display.nope", "nope.nope", "display")
	if _, ok := m["nope"]; ok {
		t.Errorf("invented a key for an unknown path: %v", m)
	}
}

// Slices and maps go whole or not at all: half a list would mean the rest is
// gone, not "the rest as shipped".
func TestMinimalWritesCollectionsWhole(t *testing.T) {
	c := Default()
	c.Order = []string{"git"}

	m := Minimal(c)

	if got, ok := m["order"].([]string); !ok || len(got) != 1 || got[0] != "git" {
		t.Errorf("order = %v, want the whole replacement list", m["order"])
	}
}

// A field with no toml tag can never be written, so a config carrying one is a
// setting the wizard would silently drop.
func TestEveryConfigFieldIsWritable(t *testing.T) {
	var walk func(t reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			name := tomlName(f)
			if name == "" {
				t.Errorf("%s%s has no toml tag, so Minimal can never write it", path, f.Name)
				continue
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path+name+".")
			}
		}
	}
	walk(reflect.TypeOf(Config{}), "")
}
