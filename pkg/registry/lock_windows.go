//go:build windows

package registry

import (
	"fmt"
	"os"
)

type storeLock struct {
	file *os.File
}

func openStoreLock(path string) (*storeLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening store lock: %w", err)
	}

	return &storeLock{file: file}, nil
}

func (l *storeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("closing store lock: %w", err)
	}

	return nil
}
