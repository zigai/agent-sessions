//go:build linux || darwin

package registry

import (
	"errors"
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
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("locking store: %w", err),
				fmt.Errorf("closing store lock: %w", closeErr),
			)
		}

		return nil, fmt.Errorf("locking store: %w", err)
	}

	return &storeLock{file: file}, nil
}

func (l *storeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlocking store: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing store lock: %w", closeErr)
	}

	return nil
}
