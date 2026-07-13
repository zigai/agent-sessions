//go:build !linux && !darwin

package observer

import (
	"fmt"
	"os"
)

func openObserverLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening observer lock: %w", err)
	}
	return file, nil
}

func closeObserverLock(file *os.File) error {
	if file == nil {
		return nil
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing observer lock: %w", err)
	}
	return nil
}

func removeObserverLock(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing observer lock: %w", err)
	}
	return nil
}
