//go:build !windows

package registry

import (
	"fmt"
	"os"
	"syscall"
)

type storeLock struct {
	file *os.File
}

func openStoreLock(path string) (*storeLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening store lock: %w", err)
	}
	flockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	if flockErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locking store: %w", flockErr)
	}

	return &storeLock{file: file}, nil
}

func (l *storeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if err != nil {
		return fmt.Errorf("unlocking store: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing store lock: %w", closeErr)
	}

	return nil
}
