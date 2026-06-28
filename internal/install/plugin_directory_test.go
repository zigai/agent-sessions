package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePluginDirectoryRollbackRestoresExistingDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "plugin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("writing old plugin file: %v", err)
	}

	staged := filepath.Join(root, "staged")
	if mkdirErr := os.MkdirAll(staged, 0o700); mkdirErr != nil {
		t.Fatalf("creating staged dir: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(staged, "new.txt"), []byte("new"), 0o600); writeErr != nil {
		t.Fatalf("writing staged plugin file: %v", writeErr)
	}

	plugin := pluginDirectoryInstall{dir: dir, markerFile: "", files: nil, snippetOrder: nil}
	rollback, _, err := plugin.replace(staged)
	if err != nil {
		t.Fatalf("plugin.replace returned error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "new.txt")); statErr != nil {
		t.Fatalf("expected staged plugin to be installed: %v", statErr)
	}
	if rollbackErr := rollback(); rollbackErr != nil {
		t.Fatalf("rollback returned error: %v", rollbackErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "old.txt")); statErr != nil {
		t.Fatalf("expected old plugin to be restored: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged plugin file to be removed, stat err=%v", statErr)
	}
}

func TestPluginDirectoryNeedsUpdateDetectsStaleFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{"plugin.json": "{}\n"}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writing expected plugin file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.js"), []byte("old"), 0o600); err != nil {
		t.Fatalf("writing stale plugin file: %v", err)
	}

	plugin := pluginDirectoryInstall{dir: dir, markerFile: "", files: files, snippetOrder: nil}
	changed, err := plugin.needsUpdate()
	if err != nil {
		t.Fatalf("plugin.needsUpdate returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected stale generated file to require plugin directory replacement")
	}
}
