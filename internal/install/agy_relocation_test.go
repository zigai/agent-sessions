package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	errRestoreLegacyAgyTest = errors.New("restore legacy Agy backup failed")
	errRemoveLegacyAgyTest  = errors.New("remove legacy Agy backup failed")
)

func TestCleanupLegacyAgyReportsManifestAndRestoreFailures(t *testing.T) {
	t.Parallel()

	pluginDir, legacyDir, _ := setupLegacyAgyRelocation(t, []byte("{"))
	ops := agyRelocationOps{
		rename: func(oldPath string, newPath string) error {
			if newPath == legacyDir && strings.Contains(filepath.Base(oldPath), ".agent-sessions-state.legacy-") {
				return errRestoreLegacyAgyTest
			}

			return os.Rename(oldPath, newPath)
		},
		removeAll: os.RemoveAll,
		stat:      os.Stat,
	}
	err := cleanupLegacyAgyWithOps(pluginDir, ops)
	if !errors.Is(err, errRestoreLegacyAgyTest) {
		t.Fatalf("cleanup error = %v, want restore failure", err)
	}
	if !strings.Contains(err.Error(), ".agent-sessions-state.legacy-") {
		t.Fatalf("cleanup error omits retained backup path: %v", err)
	}
}

func TestCleanupLegacyAgyReportsBackupRemovalAfterManifestDisappears(t *testing.T) {
	t.Parallel()

	manifest := []byte(`{"imports":[{"name":"agent-sessions-state","source":"antigravity","imported_at":"","components":["hooks"]}]}`)
	pluginDir, _, manifestPath := setupLegacyAgyRelocation(t, manifest)
	ops := agyRelocationOps{
		rename: os.Rename,
		removeAll: func(path string) error {
			if strings.Contains(filepath.Base(path), ".agent-sessions-state.legacy-") {
				return errRemoveLegacyAgyTest
			}

			return os.RemoveAll(path)
		},
		stat: func(path string) (os.FileInfo, error) {
			if path == manifestPath {
				return nil, os.ErrNotExist
			}

			return os.Stat(path)
		},
	}
	err := cleanupLegacyAgyWithOps(pluginDir, ops)
	if !errors.Is(err, errRemoveLegacyAgyTest) {
		t.Fatalf("cleanup error = %v, want backup removal failure", err)
	}
	if !strings.Contains(err.Error(), ".agent-sessions-state.legacy-") {
		t.Fatalf("cleanup error omits retained backup path: %v", err)
	}
}

func setupLegacyAgyRelocation(t *testing.T, manifest []byte) (string, string, string) {
	t.Helper()
	home := t.TempDir()
	pluginDir := filepath.Join(home, ".gemini", "antigravity-cli", "plugins", legacyAgyPluginName)
	legacyDir, manifestPath, ok := legacyAgyPaths(pluginDir)
	if !ok {
		t.Fatal("test plugin path did not select legacy Agy relocation")
	}
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacyDir, legacyAgyMarkerFile)
	if err := os.WriteFile(marker, []byte(managedMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	return pluginDir, legacyDir, manifestPath
}
