package reportqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirMode           = 0o700
	fileMode          = 0o600
	maxQueueFileBytes = 4 << 20
)

var (
	errEnvelopeVersion   = errors.New("unsupported envelope version")
	errEnvelopeKind      = errors.New("unsupported queue item kind")
	errEnvelopeID        = errors.New("invalid queue item id")
	errQueueFileTooLarge = errors.New("queue file exceeds 4 MiB limit")
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
	return writeJSON(path, value, false)
}

func writeJSONExclusive(path string, value any) error {
	return writeJSON(path, value, true)
}

func writeJSON(path string, value any, exclusive bool) error {
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
	if err := publishQueueFile(tempPath, path, exclusive); err != nil {
		return err
	}
	keep = true

	return syncDir(dir)
}

func publishQueueFile(tempPath, path string, exclusive bool) error {
	if !exclusive {
		if err := os.Rename(tempPath, path); err != nil {
			return fmt.Errorf("renaming queue item: %w", err)
		}
		return nil
	}
	if err := os.Link(tempPath, path); err != nil {
		return fmt.Errorf("publishing queue item: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("removing temp queue item: %w", err)
	}
	return nil
}

func validateEnvelopeID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) || filepath.Base(id) != id {
		return fmt.Errorf("%w: %q", errEnvelopeID, id)
	}
	return nil
}

func readEnvelope(path string) (Envelope, error) {
	data, err := readQueueFile(path)
	if err != nil {
		return Envelope{}, fmt.Errorf("reading queue item: %w", err)
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("parsing queue item %s: %w", path, err)
	}

	if envelope.Version != EnvelopeVersion {
		return Envelope{}, fmt.Errorf("%w: got %d, expected %d", errEnvelopeVersion, envelope.Version, EnvelopeVersion)
	}
	if envelope.Kind != KindReport {
		return Envelope{}, fmt.Errorf("%w: %q", errEnvelopeKind, envelope.Kind)
	}
	if err := validateEnvelopeID(envelope.ID); err != nil {
		return Envelope{}, err
	}
	if filepath.Base(path) != envelope.ID+".json" {
		return Envelope{}, fmt.Errorf("%w: id %q does not match filename", errEnvelopeID, envelope.ID)
	}

	return envelope, nil
}

func readQueueFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening queue file: %w", err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxQueueFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading queue file: %w", err)
	}
	if len(data) > maxQueueFileBytes {
		return nil, errQueueFileTooLarge
	}
	return data, nil
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
