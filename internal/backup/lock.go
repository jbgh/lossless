package backup

import (
	"errors"
	"os"
	"syscall"
)

var ErrLocked = errors.New("backup already running")

// Lock takes an exclusive, non-blocking flock on <home>/backup.lock so the
// daemon's scheduled run and a manual command never overlap.
func Lock(home string) (release func(), err error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath(home), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrLocked
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
