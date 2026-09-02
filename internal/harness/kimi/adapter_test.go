package kimi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKimiCodeSessionPathReturnsCurrentSessionDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", home)
	sessionPath := filepath.Join(home, "sessions", "work-hash", "wanted")
	if err := os.MkdirAll(sessionPath, 0o700); err != nil {
		t.Fatal(err)
	}

	path, err := kimiCodeSessionPath("wanted")
	if err != nil {
		t.Fatal(err)
	}
	if path != sessionPath {
		t.Fatalf("session path = %q, want %q", path, sessionPath)
	}
}

func TestKimiCodeSessionPathRejectsPathTraversal(t *testing.T) {
	t.Setenv("KIMI_SHARE_DIR", t.TempDir())

	path, err := kimiCodeSessionPath("../wanted")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("session path = %q, want empty", path)
	}
}
