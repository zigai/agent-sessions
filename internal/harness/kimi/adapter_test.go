package kimi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPayloadDefaultsPreservesSessionLookupFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", home)
	// A regular file where the sessions directory belongs produces a portable
	// filesystem error, including when the tests run with elevated permissions.
	if err := os.WriteFile(filepath.Join(home, "sessions"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New().PayloadDefaults(map[string]any{"session_id": "wanted"})
	if _, ok := errors.AsType[*os.PathError](err); !ok {
		t.Fatalf("error = %v, want session lookup path error", err)
	}
}

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
