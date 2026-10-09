//go:build linux || darwin || freebsd

package termwidth

import (
	"os"
	"syscall"
	"unsafe"
)

// console asks the controlling terminal for its size. /dev/tty is opened
// rather than using fd 0-2 because all three are pipes under Claude Code.
func console() int {
	f, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return 0
	}
	defer f.Close()

	var ws struct{ row, col, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0
	}
	return int(ws.col)
}
