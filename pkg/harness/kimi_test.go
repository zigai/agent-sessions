package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKimiCodeSessionPathReturnsIndexedDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	index := "{\"sessionId\":\"other\",\"sessionDir\":\"/tmp/other\"}\n" +
		"{\"sessionId\":\"wanted\",\"sessionDir\":\"/tmp/wanted\"}\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := kimiCodeSessionPath("wanted")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/wanted" {
		t.Fatalf("session path = %q, want /tmp/wanted", path)
	}
}

func TestKimiCodeSessionPathSurfacesScannerFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	oversizedLine := strings.Repeat("x", kimiCodeSessionIndexMaxBufferSize+1)
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(oversizedLine), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := kimiCodeSessionPath("wanted"); err == nil {
		t.Fatal("expected scanner failure")
	}
}
