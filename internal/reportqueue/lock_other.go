//go:build !linux && !darwin

package reportqueue

import (
	"errors"
	"fmt"
	"os"
)

var ErrLocked = errors.New("queue is locked")

type queueLock struct {
	file *os.File
	path string
}

func tryOpenQueueLock(path string) (*queueLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, fileMode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("opening queue lock: %w", err)
	}
	return &queueLock{file: file, path: path}, nil
}

func (l *queueLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		if removeErr != nil {
			return fmt.Errorf("closing queue lock: %w; removing queue lock: %v", closeErr, removeErr)
		}
		return fmt.Errorf("closing queue lock: %w", closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("removing queue lock: %w", removeErr)
	}
	return nil
}
