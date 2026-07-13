package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	legacyAgyPluginName   = "agent-sessions-state"
	legacyAgyMarkerFile   = ".agent-sessions-managed"
	legacyAgyManifestName = "import_manifest.json"
	legacyAgySource       = "antigravity"
)

// legacyAgyPaths returns old plugin and manifest paths when the active plan
// targets ~/.gemini/antigravity-cli. Paths outside that exact layout are left
// untouched, including user-configured plugin directories.
func legacyAgyPaths(pluginDir string) (string, string, bool) {
	clean := filepath.Clean(pluginDir)
	if filepath.Base(clean) != legacyAgyPluginName || filepath.Base(filepath.Dir(clean)) != "plugins" {
		return "", "", false
	}
	cliDir := filepath.Dir(filepath.Dir(clean))
	if filepath.Base(cliDir) != "antigravity-cli" || filepath.Base(filepath.Dir(cliDir)) != ".gemini" {
		return "", "", false
	}

	home := filepath.Dir(filepath.Dir(cliDir))
	legacyRoot := filepath.Join(home, ".gemini", "config")
	return filepath.Join(legacyRoot, "plugins", legacyAgyPluginName), filepath.Join(legacyRoot, legacyAgyManifestName), true
}

func legacyAgyPluginManaged(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking legacy Agy plugin: %w", err)
	}
	return (pluginDirectoryInstall{dir: path, markerFile: legacyAgyMarkerFile, files: nil, snippetOrder: nil}).managed()
}

func legacyAgyNeedsCleanup(pluginDir string) (bool, error) {
	legacyDir, manifestPath, ok := legacyAgyPaths(pluginDir)
	if !ok {
		return false, nil
	}

	managed, err := legacyAgyPluginManaged(legacyDir)
	if err != nil {
		return false, err
	}
	if managed {
		return true, nil
	}

	manifest, err := readImportManifest(manifestPath)
	if err != nil {
		return false, err
	}
	for _, item := range manifest.Imports {
		if item.Name == legacyAgyPluginName && item.Source == legacyAgySource {
			return true, nil
		}
	}

	return false, nil
}

// cleanupLegacyAgy removes only the old artifact that carries our ownership
// marker and only our import-manifest entry. It is called after the new
// directory has been staged and installed, so a failed staging operation never
// destroys the old integration.
//
//nolint:gocognit,cyclop // cleanup validates ownership and manifest entries before mutation
func cleanupLegacyAgy(pluginDir string) error {
	legacyDir, manifestPath, ok := legacyAgyPaths(pluginDir)
	if !ok {
		return nil
	}

	managed, err := legacyAgyPluginManaged(legacyDir)
	if err != nil {
		return err
	}

	var backupDir string
	if managed {
		backupDir, err = os.MkdirTemp(filepath.Dir(legacyDir), "."+legacyAgyPluginName+".legacy-*")
		if err != nil {
			return fmt.Errorf("staging legacy Agy plugin removal: %w", err)
		}
		if err := os.Remove(backupDir); err != nil {
			return fmt.Errorf("preparing legacy Agy backup path: %w", err)
		}
		if err := os.Rename(legacyDir, backupDir); err != nil {
			return fmt.Errorf("staging legacy Agy plugin removal: %w", err)
		}
	}
	restored := false
	restore := func() {
		if restored || backupDir == "" {
			return
		}
		restored = true
		_ = os.Rename(backupDir, legacyDir)
	}
	defer func() {
		if backupDir != "" && !restored {
			_ = os.RemoveAll(backupDir)
		}
	}()

	manifest, err := readImportManifest(manifestPath)
	if err != nil {
		restore()
		return err
	}
	filtered := manifest.Imports[:0]
	removed := false
	for _, item := range manifest.Imports {
		if item.Name == legacyAgyPluginName && item.Source == legacyAgySource {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		if backupDir != "" {
			if err := os.RemoveAll(backupDir); err != nil {
				restore()
				return fmt.Errorf("removing legacy Agy backup: %w", err)
			}
		}
		return nil
	}
	manifest.Imports = filtered

	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if backupDir != "" {
				_ = os.RemoveAll(backupDir)
			}
			return nil
		}
		restore()
		return fmt.Errorf("checking legacy Agy import manifest: %w", err)
	}
	if err := writeImportManifest(manifestPath, manifest); err != nil {
		restore()
		return fmt.Errorf("removing legacy Agy import entry: %w", err)
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("removing legacy Agy backup: %w", err)
		}
	}

	return nil
}
