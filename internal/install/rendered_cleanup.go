package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func staleManagedRenderedFiles(dir string, expected map[string]string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("checking rendered artifact directory %s: %w", dir, err)
	}

	stale := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("relativizing rendered artifact path: %w", err)
		}
		if _, ok := expected[relative]; ok {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // traversal is confined to the configured integration directory
		if err != nil {
			return fmt.Errorf("reading rendered artifact %s: %w", path, err)
		}
		if classifyArtifactContent(string(data)) != ArtifactForeign {
			stale = append(stale, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking rendered artifact directory %s: %w", dir, err)
	}

	return stale, nil
}
