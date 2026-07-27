package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/PouriahLabs/claude-statusline/internal/config"
	"github.com/PouriahLabs/claude-statusline/internal/gitinfo"
)

// Uninstall reverses what init did: the Claude Code wiring, the config file and
// the cache. It deliberately does NOT remove two things it installed:
//
//   - The binary. On Windows a running executable is locked, so it cannot
//     delete itself; printing the path is honest where a half-working
//     self-delete would not be.
//   - The Nerd Font, and any terminal config pointing at it. Those are
//     general-purpose -- the user very likely wants to keep the font they
//     picked -- and the patcher left a timestamped backup either way.
func (w *Wizard) Uninstall() error {
	w.printf("\nclaude-statusline uninstall\n===========================\n\n")

	w.unwireClaude()
	w.removeConfig()
	w.removeCache()

	w.printf("\nStill on disk, remove by hand if you want them gone:\n")
	if exe, err := os.Executable(); err == nil {
		if runtime.GOOS == "windows" {
			w.printf("  the binary   %s\n", exe)
			w.printf("               (a running .exe can't delete itself -- close this\n")
			w.printf("               window first, then delete the folder)\n")
		} else {
			w.printf("  the binary   rm %s\n", exe)
		}
	}
	w.printf("  the font     kept on purpose; it's a normal font you may want\n")
	w.printf("\nRestart Claude Code to drop the status line.\n")
	return nil
}

// unwireClaude removes the statusLine key, but only when it actually points at
// this tool -- a user who has since wired up a different status line must not
// lose it to our uninstaller.
func (w *Wizard) unwireClaude() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".claude", "settings.json")

	b, err := os.ReadFile(path)
	if err != nil {
		w.printf("Claude Code : no %s, nothing to unwire\n", path)
		return
	}
	if jsonComment.Match(b) {
		w.printf("Claude Code : %s has comments, so I won't rewrite it.\n", path)
		w.printf("              Remove the \"statusLine\" key yourself.\n")
		return
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		w.printf("Claude Code : %s isn't strict JSON (%v).\n", path, err)
		w.printf("              Remove the \"statusLine\" key yourself.\n")
		return
	}
	sl, ok := root["statusLine"].(map[string]any)
	if !ok {
		w.printf("Claude Code : no statusLine set, nothing to unwire\n")
		return
	}
	cmd, _ := sl["command"].(string)
	if !strings.Contains(strings.ToLower(cmd), "claude-statusline") {
		w.printf("Claude Code : statusLine points at something else, leaving it:\n")
		w.printf("              %s\n", cmd)
		return
	}
	if !w.ask(fmt.Sprintf("Remove statusLine from %s? A backup will be written.", path), true) {
		w.printf("              left alone\n")
		return
	}
	_ = os.WriteFile(path+".bak-"+time.Now().Format("20060102-150405"), b, 0o644)
	delete(root, "statusLine")
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		w.printf("              write failed: %v\n", err)
		return
	}
	w.printf("              removed\n")
}

func (w *Wizard) removeConfig() {
	dir := config.Dir()
	if _, err := os.Stat(dir); err != nil {
		w.printf("Config      : none at %s\n", dir)
		return
	}
	if !w.ask(fmt.Sprintf("Delete %s and everything in it?", dir), true) {
		w.printf("              kept\n")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		w.printf("              delete failed: %v\n", err)
		return
	}
	w.printf("              deleted\n")
}

// removeCache needs no prompt: it is derived data that regenerates itself.
func (w *Wizard) removeCache() {
	dir := gitinfo.CacheDir()
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if err := os.RemoveAll(dir); err == nil {
		w.printf("Cache       : cleared %s\n", dir)
	}
}
