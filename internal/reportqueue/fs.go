package reportqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

func ensureQueueDirs(q Queue) error {
	for _, dir := range []string{q.root, q.pendingDir(), q.processingDir(), q.deadDir()} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return fmt.Errorf("creating queue directory %s: %w", dir, err)
		}
	}

	return nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding queue item: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("creating queue directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp queue item: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if keep {
			return
		}
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(fileMode); err != nil {
		return fmt.Errorf("setting queue item permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("writing queue item: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("syncing queue item: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing queue item: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("renaming queue item: %w", err)
	}
	keep = true

	return syncDir(dir)
}

func readEnvelope(path string) (Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, fmt.Errorf("reading queue item: %w", err)
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("parsing queue item %s: %w", path, err)
	}

	return envelope, nil
}

func renameDurable(from string, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), dirMode); err != nil {
		return fmt.Errorf("creating destination queue directory: %w", err)
	}
	fromDir := filepath.Dir(from)
	toDir := filepath.Dir(to)
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("renaming queue item: %w", err)
	}
	if err := syncDir(fromDir); err != nil {
		return err
	}
	if toDir != fromDir {
		return syncDir(toDir)
	}

	return nil
}

func removeDurable(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing queue item: %w", err)
	}

	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening directory for sync: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing directory: %w", err)
	}

	return nil
}
