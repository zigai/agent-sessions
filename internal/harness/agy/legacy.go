package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/aht/internal/harness"
)

const (
	legacyAgyPluginName   = "aht-state"
	legacyAgyMarkerFile   = ".aht-managed"
	legacyAgyManifestName = "import_manifest.json"
	legacyAgySource       = "antigravity"
)

type legacyMigration struct{}

func (legacyMigration) NeedsCleanup(pluginDir string) (bool, error) {
	return legacyNeedsCleanup(pluginDir)
}

func (legacyMigration) Cleanup(pluginDir string) error {
	return cleanupLegacy(pluginDir)
}

type agyRelocationOps struct {
	rename    func(string, string) error
	removeAll func(string) error
	stat      func(string) (os.FileInfo, error)
}

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
	marker, err := os.ReadFile(filepath.Join(path, legacyAgyMarkerFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading legacy Agy plugin marker: %w", err)
	}
	return strings.Contains(string(marker), harness.ManagedMarker), nil
}

func legacyNeedsCleanup(pluginDir string) (bool, error) {
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

// cleanupLegacy removes only the old artifact that carries our ownership
// marker and only our import-manifest entry. It is called after the new
// directory has been staged and installed, so a failed staging operation never
// destroys the old integration.
func cleanupLegacy(pluginDir string) error {
	return cleanupLegacyAgyWithOps(pluginDir, agyRelocationOps{
		rename:    os.Rename,
		removeAll: os.RemoveAll,
		stat:      os.Stat,
	})
}

//nolint:gocognit,cyclop // injectable file operations cover rollback failures without package-global test seams
func cleanupLegacyAgyWithOps(pluginDir string, ops agyRelocationOps) error {
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
		if err := ops.rename(legacyDir, backupDir); err != nil {
			return fmt.Errorf("staging legacy Agy plugin removal: %w", err)
		}
	}
	restore := func(cause error) error {
		if backupDir == "" {
			return cause
		}
		if restoreErr := ops.rename(backupDir, legacyDir); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("restoring legacy Agy plugin backup %s: %w", backupDir, restoreErr))
		}
		backupDir = ""

		return cause
	}
	discard := func() error {
		if backupDir == "" {
			return nil
		}
		if err := ops.removeAll(backupDir); err != nil {
			return fmt.Errorf("removing legacy Agy backup %s: %w", backupDir, err)
		}
		backupDir = ""

		return nil
	}

	manifest, err := readImportManifest(manifestPath)
	if err != nil {
		return restore(err)
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
		if err := discard(); err != nil {
			return restore(err)
		}
		return nil
	}
	manifest.Imports = filtered

	if _, err := ops.stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return discard()
		}
		return restore(fmt.Errorf("checking legacy Agy import manifest: %w", err))
	}
	if err := writeImportManifest(manifestPath, manifest); err != nil {
		return restore(fmt.Errorf("removing legacy Agy import entry: %w", err))
	}

	return discard()
}

type importManifest struct {
	Imports []importEntry `json:"imports"`
}

type importEntry struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ImportedAt string   `json:"imported_at"`
	Components []string `json:"components"`
}

func readImportManifest(path string) (importManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return importManifest{Imports: nil}, nil
		}
		return importManifest{}, fmt.Errorf("reading import manifest: %w", err)
	}
	var manifest importManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return importManifest{}, fmt.Errorf("parsing import manifest: %w", err)
	}
	return manifest, nil
}

func writeImportManifest(path string, manifest importManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding import manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating import manifest temporary file: %w", err)
	}
	tempPath := file.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("setting import manifest permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing import manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("syncing import manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing import manifest: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replacing import manifest: %w", err)
	}
	return nil
}
