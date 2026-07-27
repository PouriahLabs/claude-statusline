// Package terminal detects installed terminals and can point them at a font.
//
// Editing someone's terminal config is invasive, so every mutation here is
// gated behind an explicit caller decision, always writes a timestamped
// backup first, and refuses to touch a JSON file that contains comments --
// re-serialising such a file would silently delete the user's annotations.
// When the tool refuses, it prints the exact snippet to paste instead.
package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Kind int

const (
	KindJSON   Kind = iota // Windows Terminal, VS Code
	KindConf               // kitty, alacritty: line-oriented key/value
	KindManual             // documented, but not safely patchable
)

type Terminal struct {
	Name     string
	Path     string
	Kind     Kind
	Snippet  string // what to paste when patching isn't possible
	jsonPath []string
	confKey  string
}

// Detect returns the terminals whose config files exist on this machine.
func Detect() []Terminal {
	home, _ := os.UserHomeDir()
	var out []Terminal

	add := func(t Terminal) {
		if t.Path != "" {
			if _, err := os.Stat(t.Path); err == nil {
				out = append(out, t)
			}
		}
	}

	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		add(Terminal{
			Name: "Windows Terminal",
			Path: filepath.Join(local, "Packages",
				"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
			Kind:     KindJSON,
			jsonPath: []string{"profiles", "defaults", "font", "face"},
			Snippet:  "\"profiles\": { \"defaults\": { \"font\": { \"face\": \"%s\" } } }",
		})
	}

	// VS Code's integrated terminal, all platforms.
	var codeDir string
	switch runtime.GOOS {
	case "windows":
		codeDir = filepath.Join(os.Getenv("APPDATA"), "Code", "User")
	case "darwin":
		codeDir = filepath.Join(home, "Library", "Application Support", "Code", "User")
	default:
		codeDir = filepath.Join(home, ".config", "Code", "User")
	}
	add(Terminal{
		Name:     "VS Code",
		Path:     filepath.Join(codeDir, "settings.json"),
		Kind:     KindJSON,
		jsonPath: []string{"terminal.integrated.fontFamily"},
		Snippet:  "\"terminal.integrated.fontFamily\": \"%s\"",
	})

	add(Terminal{
		Name:    "kitty",
		Path:    filepath.Join(home, ".config", "kitty", "kitty.conf"),
		Kind:    KindConf,
		confKey: "font_family",
		Snippet: "font_family %s",
	})
	add(Terminal{
		Name:    "Alacritty",
		Path:    filepath.Join(home, ".config", "alacritty", "alacritty.toml"),
		Kind:    KindManual,
		Snippet: "[font.normal]\nfamily = \"%s\"",
	})
	add(Terminal{
		Name:    "WezTerm",
		Path:    filepath.Join(home, ".config", "wezterm", "wezterm.lua"),
		Kind:    KindManual,
		Snippet: "config.font = wezterm.font(\"%s\")",
	})

	if runtime.GOOS == "darwin" {
		// Terminal.app and iTerm2 store fonts in binary plists / archived
		// objects. Patching them safely is not worth the risk.
		out = append(out, Terminal{
			Name:    "Terminal.app / iTerm2",
			Kind:    KindManual,
			Snippet: "Settings > Profiles > Text > Font: %s",
		})
	}
	return out
}

func (t Terminal) Instructions(family string) string {
	return fmt.Sprintf(t.Snippet, family)
}

// Backup writes a timestamped copy beside the original and returns its path.
func (t Terminal) Backup() (string, error) {
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return "", err
	}
	dst := fmt.Sprintf("%s.bak-%s", t.Path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

var commentLine = regexp.MustCompile(`(?m)^\s*//`)

// CanPatch reports whether an automated edit is safe, and why not if it isn't.
func (t Terminal) CanPatch() (bool, string) {
	switch t.Kind {
	case KindManual:
		return false, "this terminal's config can't be edited safely by a script"
	case KindConf:
		return true, ""
	}
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return false, err.Error()
	}
	if commentLine.Match(b) {
		return false, "the file contains // comments; rewriting it as JSON would delete them"
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return false, "the file isn't strict JSON (" + err.Error() + ")"
	}
	return true, ""
}

// Patch points the terminal at family. Callers must confirm with the user and
// take a Backup first.
func (t Terminal) Patch(family string) error {
	switch t.Kind {
	case KindJSON:
		return t.patchJSON(family)
	case KindConf:
		return t.patchConf(family)
	}
	return fmt.Errorf("%s: not patchable", t.Name)
}

func (t Terminal) patchJSON(family string) error {
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return err
	}
	// Walk/create the nested path, then set the leaf.
	cur := root
	for i, k := range t.jsonPath {
		if i == len(t.jsonPath)-1 {
			cur[k] = family
			break
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	out, err := json.MarshalIndent(root, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.Path, append(out, '\n'), 0o644)
}

func (t Terminal) patchConf(family string) error {
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), t.confKey+" ") {
			lines[i] = t.confKey + " " + family
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, t.confKey+" "+family)
	}
	return os.WriteFile(t.Path, []byte(strings.Join(lines, "\n")), 0o644)
}
