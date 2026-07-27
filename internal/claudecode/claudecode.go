// Package claudecode reads Claude Code's own settings, which this tool has to
// understand from two directions: the wizard writes the statusLine entry, and
// doctor reports on it. Both need the same answers to "where does the file
// live", "what is configured now" and "is that command this binary" -- and a
// disagreement between them is a bug that shows up as doctor blessing a wiring
// the wizard would offer to replace.
package claudecode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SettingsPath is Claude Code's user settings file.
func SettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// StatusLineCommand returns the configured statusLine command, or "" if there
// is none to lose. Claude Code accepts shapes this tool does not write, so an
// entry that cannot be read as {type, command} still counts as occupied --
// reporting "no statusLine" for one is how a replace becomes a silent clobber.
func StatusLineCommand(root map[string]any) string {
	v, ok := root["statusLine"]
	if !ok || v == nil {
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	if cmd, ok := m["command"].(string); ok && cmd != "" {
		return cmd
	}
	if len(m) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ResolveCommand extracts the executable from a statusLine command. The
// command may carry arguments, may quote a path containing spaces, and on
// Windows may still be backslash-escaped as it appeared in JSON.
//
// The result is a program name, not necessarily a path. A status line is very
// often a script run through an interpreter --
//
//	powershell -NoProfile -File "C:\Users\me\.claude\statusline.ps1"
//
// -- where the executable is "powershell", found on PATH rather than on disk
// relative to anything. Callers that want to know whether it can actually run
// must ask Live, not stat this.
func ResolveCommand(cmd string) string {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return ""
	}
	// An unquoted path containing spaces is the case a plain split gets wrong,
	// and on Windows it is the common one, so the whole string wins whenever it
	// names something that exists.
	if _, err := os.Stat(c); err == nil {
		return c
	}
	if c[0] == '"' {
		// Undo shell escaping, so what comes back is the path itself and
		// SameCommand can compare it against a real file.
		//
		// Only the four characters a shell treats as special inside double
		// quotes are unescaped. A blanket "\x means x" would eat the backslashes
		// out of a hand-written "C:\Users\me\statusline.ps1" -- which is the
		// commonest quoted command there is, and one this code has to read
		// correctly to avoid calling a working status line broken.
		var b strings.Builder
		for i := 1; i < len(c); i++ {
			if c[i] == '\\' && i+1 < len(c) {
				switch c[i+1] {
				case '"', '\\', '$', '`':
					i++
					b.WriteByte(c[i])
					continue
				}
			}
			if c[i] == '"' {
				break
			}
			b.WriteByte(c[i])
		}
		return b.String()
	}
	if i := strings.IndexByte(c, ' '); i > 0 {
		return c[:i]
	}
	return c
}

// CommandFor renders a path to this binary as a statusLine command that
// survives the shell Claude Code runs it through.
//
// Writing the raw path was wrong on Windows in a way that produced no error
// anywhere. Claude Code executes the configured command via an sh-style shell,
// where a backslash is an escape character: an unquoted
//
//	C:\Users\Pouri\AppData\Local\Programs\claude-statusline\claude-statusline.exe
//
// arrives as C:UsersPouriAppData... -- every \U, \P, \A eaten -- and silently
// does not exist. The status line drew nothing at all, and since the binary ran
// perfectly by hand, nothing pointed at the wiring.
//
// Forward slashes are accepted by every Windows path API and pass through the
// shell untouched. Quotes cover a path containing a space, which forward
// slashes alone do not, and which the default install path acquires as soon as
// the account name has one.
func CommandFor(exe string) string { return commandFor(exe, runtime.GOOS == "windows") }

// commandFor takes the platform explicitly so the Windows rule is exercised by
// the tests on every runner, not only on the one where the bug appears.
func commandFor(exe string, windows bool) string {
	c := exe
	if windows {
		// Not filepath.ToSlash: that switches on the compiling platform, so it
		// is a no-op everywhere the failure cannot be reproduced.
		c = strings.ReplaceAll(c, `\`, "/")
	}
	if strings.ContainsAny(c, " \t") {
		// Shell double-quoting: a backslash and a quote both have to be escaped
		// inside, in that order, or a path carrying either produces a command
		// line that ends somewhere other than where it should.
		q := strings.ReplaceAll(c, `\`, `\\`)
		q = strings.ReplaceAll(q, `"`, `\"`)
		return `"` + q + `"`
	}
	return c
}

// SameCommand reports whether a statusLine command runs the binary at exe.
// Compares the resolved file rather than the string, so a reinstall through a
// symlink or a different spelling of the same path is not mistaken for a
// different tool.
func SameCommand(cmd, exe string) bool {
	c := ResolveCommand(cmd)
	if c == "" {
		return false
	}
	if c == exe {
		return true
	}
	return SameFile(c, exe)
}

// Live reports whether a statusLine command can actually run.
//
// It decides how hard to push back on replacing what is already configured, so
// the two failure modes are not symmetric. Calling a dead pointer live means
// offering to repair something that is already fine -- the user says no and
// nothing is lost. Calling a live command dead means offering, by default, to
// take over a status line that works. This resolves either way it can, and
// treats anything it cannot judge as live.
//
// A file on disk is the easy case. A bare name is the one that matters: a
// status line is commonly a script run through an interpreter, where the
// executable is "powershell" or "node" and lives on PATH. Stat alone reports
// every such command as missing, which is exactly the mistake that made this
// function dangerous.
func Live(cmd string) bool {
	c := ResolveCommand(cmd)
	if c == "" {
		return false
	}
	if _, err := os.Stat(c); err == nil {
		return true
	}
	if _, err := exec.LookPath(c); err == nil {
		return true
	}
	// A bare name that PATH does not know is still ambiguous -- PATH here is
	// this process's, not the shell's that Claude Code will use. Only a command
	// that looks like a path, and is not there, is confidently dead.
	return !strings.ContainsAny(c, `/\`)
}

// SameFile reports whether two paths name the same file on disk.
func SameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
