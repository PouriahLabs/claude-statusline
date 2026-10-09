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
// The terminal is asked before COLUMNS because it is live: COLUMNS is only
// present if the user exported it, and then reflects the size at export time.
func Detect() (cols int, source string) {
	if c := console(); c > 0 {
		return c, "terminal"
	}
	return fromEnv(os.Getenv)
}

func fromEnv(getenv func(string) string) (int, string) {
	if n, err := strconv.Atoi(getenv("COLUMNS")); err == nil && n > 0 {
		return n, "COLUMNS"
	}
	return 0, ""
}
