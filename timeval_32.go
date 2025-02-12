// +build arm

package main

import "syscall"

func setSocketTimeout(fd int, timeout int) error {
	tv := syscall.Timeval{
		Sec:  int32(timeout / 1000),
		Usec: int32((timeout % 1000) * 1000),
	}
	return syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}
