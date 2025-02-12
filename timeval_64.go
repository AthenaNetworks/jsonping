//go:build amd64 || arm64

package main

import "syscall"

func setSocketTimeout(fd int, timeout int) error {
	tv := syscall.Timeval{
		Sec:  int64(timeout / 1000),
		Usec: int64((timeout % 1000) * 1000),
	}
	return syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}
