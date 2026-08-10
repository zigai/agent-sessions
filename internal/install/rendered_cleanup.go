package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func staleManagedRenderedFiles(dir string, expected map[string]string) ([]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening rendered artifact directory %s: %w", dir, err)
	}

	stale := make([]string, 0)
	walkErr := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative := filepath.FromSlash(path)
		if _, ok := expected[relative]; ok {
			return nil
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading rendered artifact %s: %w", filepath.Join(dir, relative), err)
		}
		if classifyArtifactContent(string(data)) != ArtifactForeign {
			stale = append(stale, filepath.Join(dir, relative))
		}
		return nil
	})
	closeErr := root.Close()
	if walkErr != nil {
		return nil, fmt.Errorf("walking rendered artifact directory %s: %w", dir, walkErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing rendered artifact directory %s: %w", dir, closeErr)
	}

	return stale, nil
}
