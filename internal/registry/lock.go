package registry

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Lock serializes metadata transactions. The private lock file is retained so
// concurrent callers always lock the same inode; the OS releases locks on exit.
func Lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("cannot create registry directory")
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("cannot open registry lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("registry is busy; retry after the current operation")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
