package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStatusLineCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{"absent", `{}`, ""},
		{"null", `{"statusLine":null}`, ""},
		{"empty object", `{"statusLine":{}}`, ""},
		{"command", `{"statusLine":{"type":"command","command":"/usr/bin/other"}}`, "/usr/bin/other"},
		{"empty command", `{"statusLine":{"type":"command","command":""}}`, "map[command: type:command]"},
		// Claude Code has shapes this tool never writes. An entry it cannot
		// parse is still an entry -- reporting "none" here is how a replace
		// silently becomes an add.
		{"unknown shape", `{"statusLine":{"type":"static","text":"hi"}}`, "map[text:hi type:static]"},
		{"not an object", `{"statusLine":"/usr/bin/other"}`, "/usr/bin/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal([]byte(tc.json), &root); err != nil {
				t.Fatalf("bad fixture: %v", err)
			}
			if got := StatusLineCommand(root); got != tc.want {
				t.Errorf("StatusLineCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSameCommand(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "claude-statusline")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Link(exe, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	for _, tc := range []struct {
		name, cmd string
		want      bool
	}{
		{"identical", exe, true},
		{"surrounding space", "  " + exe + "  ", true},
		{"same file via link", link, true},
		{"with arguments", exe + " --flag", true},
		{"different tool", "/usr/local/bin/some-other-statusline", false},
		{"different tool with args", "/usr/local/bin/other --fancy", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameCommand(tc.cmd, exe); got != tc.want {
				t.Errorf("SameCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// A path containing spaces must not be split at the first space and then
// mistaken for a different tool -- that turns a no-op re-run into a prompt to
// replace the binary with itself.
func TestSameCommandHandlesSpacesInPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "my statusline")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !SameCommand(exe, exe) {
		t.Errorf("SameCommand did not match a path containing spaces")
	}
}

func TestLive(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "other-statusline")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cmd string
		want      bool
	}{
		{"existing binary", exe, true},
		{"existing with arguments", exe + " --fancy", true},
		{"deleted binary", filepath.Join(dir, "gone"), false},
		{"deleted with arguments", filepath.Join(dir, "gone") + " --fancy", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Live(tc.cmd); got != tc.want {
				t.Errorf("Live(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// The command form that broke a real install: a script run through an
// interpreter. The executable is a name on PATH, not a file, and calling it
// dead made the wizard offer -- by default -- to take over a working bar.
func TestLiveAcceptsInterpreterCommands(t *testing.T) {
	script := filepath.Join(t.TempDir(), "statusline.ps1")
	if err := os.WriteFile(script, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sh is on PATH everywhere this test runs; powershell is the real-world case.
	for _, cmd := range []string{
		`sh ` + script,
		`sh -c "` + script + `"`,
		"powershell -NoProfile -NonInteractive -File " + strconv.Quote(script),
		"node /some/script.js",
		"python3 -u /some/script.py",
	} {
		if !Live(cmd) {
			t.Errorf("Live(%q) = false, want true -- an interpreter command is not a dead pointer", cmd)
		}
	}
}

// A quoted path with spaces must not be split at the space.
func TestResolveCommandHandlesQuotedPaths(t *testing.T) {
	for _, tc := range []struct{ cmd, want string }{
		{`"C:\Program Files\tool\sl.exe" --flag`, `C:\Program Files\tool\sl.exe`},
		{`"/usr/local/my tool/sl"`, `/usr/local/my tool/sl`},
		{`powershell -File "C:\x.ps1"`, `powershell`},
		{`/usr/bin/sl`, `/usr/bin/sl`},
	} {
		if got := ResolveCommand(tc.cmd); got != tc.want {
			t.Errorf("ResolveCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// The one case that must still read as dead: an absolute path to something gone.
func TestLiveStillCatchesADeadPath(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "uninstalled", "tool")
	for _, cmd := range []string{gone, gone + " --flag", strconv.Quote(gone) + " --flag"} {
		if Live(cmd) {
			t.Errorf("Live(%q) = true, want false -- that path does not exist", cmd)
		}
	}
}

// samePath compares two paths ignoring separator style, because CommandFor
// deliberately rewrites separators on Windows and a byte comparison would call
// that a failure.
func samePath(a, b string) bool {
	return strings.ReplaceAll(a, `\`, "/") == strings.ReplaceAll(b, `\`, "/")
}

// The bug that produced a silent, invisible failure on Windows: Claude Code
// runs the command through an sh-style shell, where an unquoted Windows path
// loses every backslash that begins an escape. C:\Users\... became C:Users...,
// which does not exist, so the bar simply never drew.
func TestCommandForSurvivesAShell(t *testing.T) {
	for _, exe := range []string{
		`C:\Users\Pouri\AppData\Local\Programs\claude-statusline\claude-statusline.exe`,
		`C:\Users\Ann Smith\AppData\Local\Programs\cs\cs.exe`,
		"/home/me/.local/bin/claude-statusline",
		"/home/my name/.local/bin/claude-statusline",
		`/home/a "quoted" dir/claude-statusline`,
	} {
		got := CommandFor(exe)

		// Whatever we write has to name the same file when read back, or the
		// next init offers to replace this binary with itself. Separator style
		// is allowed to differ -- rewriting it is the entire point.
		if resolved := ResolveCommand(got); !samePath(resolved, exe) {
			t.Errorf("CommandFor(%q) = %q, which resolves to %q", exe, got, resolved)
		}
		// A space must be quoted, or the shell splits the command in two.
		if strings.ContainsAny(exe, " \t") && !strings.HasPrefix(got, `"`) {
			t.Errorf("CommandFor(%q) = %q, a space would split the command", exe, got)
		}
	}
}

// A command we write must be recognised as ours on the next run, or the wizard
// offers to replace the binary with itself.
func TestCommandForRoundTripsThroughSameCommand(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "claude-statusline")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := CommandFor(exe)
	if !SameCommand(cmd, exe) {
		t.Errorf("SameCommand(%q, %q) = false -- init would not recognise its own wiring", cmd, exe)
	}
	if !Live(cmd) {
		t.Errorf("Live(%q) = false for a command we just wrote", cmd)
	}
}

// The Windows rule, exercised on every platform -- the failure it prevents is
// invisible on Linux and macOS, so testing it only where it reproduces means
// testing it almost never.
func TestCommandForWindowsRules(t *testing.T) {
	for _, tc := range []struct{ name, exe, want string }{
		{
			"the real failing path",
			`C:\Users\Pouri\AppData\Local\Programs\claude-statusline\claude-statusline.exe`,
			`C:/Users/Pouri/AppData/Local/Programs/claude-statusline/claude-statusline.exe`,
		},
		{
			"account name with a space still needs quoting",
			`C:\Users\Ann Smith\AppData\Local\Programs\cs\cs.exe`,
			`"C:/Users/Ann Smith/AppData/Local/Programs/cs/cs.exe"`,
		},
		{"already forward", `C:/x/y.exe`, `C:/x/y.exe`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandFor(tc.exe, true); got != tc.want {
				t.Errorf("commandFor(%q, windows) = %q, want %q", tc.exe, got, tc.want)
			}
		})
	}
	// A backslash on a unix path is a legitimate filename character, not a
	// separator, so it must be left alone.
	if got := commandFor(`/home/od\d/cs`, false); got != `/home/od\d/cs` {
		t.Errorf("unix path was rewritten: %q", got)
	}
}
