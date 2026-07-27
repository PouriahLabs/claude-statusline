package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadTOML(t *testing.T, body string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

// The README offers "#rrggbb or a 0-255 index". TOML has a real integer type,
// so the unquoted form has to decode -- and because Load returns the defaults
// on any error, rejecting it cost the user their whole theme, not just one key.
func TestColorValueAcceptsBothForms(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"hex", "[theme]\ndir = \"#ff0000\"\n", "#ff0000"},
		{"quoted index", "[theme]\ndir = \"24\"\n", "24"},
		{"bare index", "[theme]\ndir = 24\n", "24"},
		{"bare zero", "[theme]\ndir = 0\n", "0"},
		{"bare max", "[theme]\ndir = 255\n", "255"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadTOML(t, tc.body)
			if err != nil {
				t.Fatalf("failed to load: %v", err)
			}
			if string(c.Theme.Dir) != tc.want {
				t.Errorf("dir = %q, want %q", c.Theme.Dir, tc.want)
			}
			// The rest of the theme must survive a form it now understands.
			if c.Theme.Git != Default().Theme.Git {
				t.Errorf("unrelated key lost: git = %q", c.Theme.Git)
			}
		})
	}
}

func TestColorValueRejectsNonsense(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"out of range high", "[theme]\ndir = 256\n"},
		{"out of range low", "[theme]\ndir = -1\n"},
		{"float", "[theme]\ndir = 1.5\n"},
		{"boolean", "[theme]\ndir = true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadTOML(t, tc.body); err == nil {
				t.Errorf("accepted %s without complaint", tc.name)
			}
		})
	}
}

// Resolve is tested by behaviour rather than against the shipped values, so
// retuning the curated set does not require editing this.
func TestResolveAppliesOverridesOnlyAt256(t *testing.T) {
	d := Default()

	if got := d.Resolve(false); got != d.Theme {
		t.Errorf("truecolor theme was modified:\n got %+v\nwant %+v", got, d.Theme)
	}

	at256 := d.Resolve(true)
	rt, ro, rb := reflect.ValueOf(at256), reflect.ValueOf(d.Theme256), reflect.ValueOf(d.Theme)
	overridden := 0
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Type().Field(i).Name
		switch over := ro.Field(i).String(); over {
		case "":
			if rt.Field(i).String() != rb.Field(i).String() {
				t.Errorf("%s has no override but changed to %q", name, rt.Field(i).String())
			}
		default:
			overridden++
			if rt.Field(i).String() != over {
				t.Errorf("%s = %q, want the %q override", name, rt.Field(i).String(), over)
			}
		}
	}
	if overridden == 0 {
		t.Error("the shipped theme sets no 256-colour overrides at all")
	}
}

func TestResolveIsAConfigurableOverride(t *testing.T) {
	c, err := loadTOML(t, "[theme_256]\ndir = 24\ncost = \"#abcdef\"\n")
	if err != nil {
		t.Fatal(err)
	}
	got := c.Resolve(true)
	if got.Dir != "24" {
		t.Errorf("dir = %q, want the 24 override", got.Dir)
	}
	if got.Cost != "#abcdef" {
		t.Errorf("cost = %q, want the hex override", got.Cost)
	}
	if c.Resolve(false).Dir != Default().Theme.Dir {
		t.Errorf("the override leaked into truecolor")
	}
}

// Guard the actual complaint: adjacent pills must not share a background.
func TestShippedThemeHasNoAdjacentCollisionAt256(t *testing.T) {
	if !strings.Contains(strings.Join(Default().Order, ","), "limits,dir") {
		t.Skip("limits and dir are no longer adjacent")
	}
	th := Default().Resolve(true)
	if th.Limits == th.Dir {
		t.Errorf("limits and dir both resolve to %q at 256 colours", th.Limits)
	}
}
