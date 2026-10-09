//go:build windows

package termwidth

import (
	"syscall"
	"unsafe"
)

var getConsoleScreenBufferInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")

type coord struct{ x, y int16 }

type consoleScreenBufferInfo struct {
	size       coord
	cursor     coord
	attributes uint16
	window     struct{ left, top, right, bottom int16 }
	maxSize    coord
}

// console opens CONOUT$ rather than using stdout, which is a pipe under Claude
// Code. It reports the visible window, not the buffer: the buffer can be far
// taller and, in the legacy console, wider than what is on screen.
func console() int {
	name, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return 0
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0
	}
	defer syscall.CloseHandle(h)

	var info consoleScreenBufferInfo
	r, _, _ := getConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.window.right-info.window.left) + 1
}
