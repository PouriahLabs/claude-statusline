// Package termwidth finds how many columns the status line has to work with.
//
// Claude Code's payload carries no terminal width, and stdout is a pipe, so
// there is nothing to ask the usual way. What is left is the controlling
// terminal itself (/dev/tty, or CONOUT$ on Windows) and the COLUMNS variable.
// Neither is guaranteed to be reachable from a status line subprocess, so a
// zero result is a normal answer and callers must treat it as "unlimited".
package termwidth

import (
	"os"
	"strconv"
)

// Detect returns the terminal width in columns and a short note on where it
// came from, or 0 and "" when it cannot be found.
//
// COLUMNS wins because Claude Code sets it for the status line subprocess from
// its own terminal. The terminal query is only the fallback: on Windows the
// subprocess gets a private console with the default 120-column window (logged
// at 120 while the real terminal and COLUMNS were 210), so asking it first
// fitted the bar to a width that does not exist.
func Detect() (cols int, source string) {
	if c, src := fromEnv(os.Getenv); c > 0 {
		return c, src
	}
	if c := console(); c > 0 {
		return c, "terminal"
	}
	return 0, ""
}

func fromEnv(getenv func(string) string) (int, string) {
	if n, err := strconv.Atoi(getenv("COLUMNS")); err == nil && n > 0 {
		return n, "COLUMNS"
	}
	return 0, ""
}
