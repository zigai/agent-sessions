//go:build windows

package registry

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type storeLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func openStoreLock(path string) (*storeLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening store lock: %w", err)
	}
	lock := &storeLock{file: file}
	lockErr := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		1,
		0,
		&lock.overlapped,
	)
	if lockErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locking store: %w", lockErr)
	}

	return lock, nil
}

func (l *storeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	closeErr := l.file.Close()
	if err != nil {
		return fmt.Errorf("unlocking store: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing store lock: %w", err)
	}

	return nil
}
