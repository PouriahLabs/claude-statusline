package segments

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Ultracode is a Claude Code session flag that opts a turn into multi-agent
// orchestration. It is NOT an effort level, despite behaving like one:
// disassembly of claude.exe 2.1.218 shows the valid levels are
//
//	["low", "medium", "high", "xhigh", "max"]
//
// with ultracode carried as a separate boolean that MAPS to xhigh
// (`HOc = {ultracode: "xhigh"}`, `applied.ultracode: boolean().optional()`).
//
// Critically, the statusLine payload does not carry it. A live capture with
// ultracode active reports plain `"effort":{"level":"xhigh"}` and no other
// field differs. So there is nothing in the input to key off, and adding an
// "ultracode" case to the effort switch would be dead code.
//
// What IS observable: when a user enables ultracode *persistently* in
// settings.json, that file records it. Reading it is the only honest
// detection available. Per-prompt use of the keyword leaves no trace the
// status line can see, and this deliberately does not pretend otherwise.
//
// Precedence: the env var wins, so users who drive ultracode per-prompt can
// still surface it from a wrapper if they want to.
const ultracodeEnv = "CLAUDE_STATUSLINE_ULTRACODE"

var (
	ultraOnce sync.Once
	ultraOn   bool
)

func ultracodeActive() bool {
	ultraOnce.Do(func() {
		if v, ok := os.LookupEnv(ultracodeEnv); ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "on":
				ultraOn = true
			}
			return
		}
		ultraOn = ultracodeInSettings()
	})
	return ultraOn
}

// jsonComment matches a whole-line // comment. Claude Code tolerates them in
// settings.json, so a strict Unmarshal can fail on a perfectly valid file.
var jsonComment = regexp.MustCompile(`(?m)^\s*//.*$`)

func ultracodeInSettings() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	var s struct {
		Ultracode *bool `json:"ultracode"`
	}
	if json.Unmarshal(b, &s) != nil {
		// Retry with line comments stripped before giving up.
		if json.Unmarshal(jsonComment.ReplaceAll(b, nil), &s) != nil {
			return false
		}
	}
	return s.Ultracode != nil && *s.Ultracode
}
