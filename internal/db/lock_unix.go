//go:build !windows

package db

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(file *os.File, exclusive bool) (bool, error) {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	for {
		err := unix.Flock(int(file.Fd()), how|unix.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, unix.EWOULDBLOCK):
			return false, nil
		case errors.Is(err, unix.EINTR):
			continue
		default:
			return false, err
		}
	}
}

func unlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
