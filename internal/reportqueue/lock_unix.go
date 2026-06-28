//go:build linux || darwin

package reportqueue

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrLocked = errors.New("queue is locked")

type queueLock struct {
	file *os.File
}

func tryOpenQueueLock(path string) (*queueLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("opening queue lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			if closeErr != nil {
				return nil, fmt.Errorf("closing queue lock after failed lock: %w", closeErr)
			}

			return nil, ErrLocked
		}
		if closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("locking queue: %w", err),
				fmt.Errorf("closing queue lock: %w", closeErr),
			)
		}

		return nil, fmt.Errorf("locking queue: %w", err)
	}

	return &queueLock{file: file}, nil
}

func (l *queueLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if err != nil {
		return fmt.Errorf("unlocking queue: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing queue lock: %w", closeErr)
	}

	return nil
}
