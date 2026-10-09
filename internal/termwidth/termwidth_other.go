//go:build !windows && !linux && !darwin && !freebsd

package termwidth

func console() int { return 0 }
