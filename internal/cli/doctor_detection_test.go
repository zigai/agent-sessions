package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorWarnsAndFallsBackForInvalidDetectionOverride(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	detectionDir := filepath.Join(configHome, "agent-sessions", "detection")
	if err := os.MkdirAll(detectionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(detectionDir, "pi.toml"), []byte("invalid ["), 0o600); err != nil {
		t.Fatal(err)
	}
	result := doctorResult{OK: true, Checks: nil, Capabilities: nil}
	(&application{}).addDetectionManifestCheck(&result)
	if len(result.Checks) != 1 || result.Checks[0].Name != "detection.manifests" || result.Checks[0].Status != doctorWarning {
		t.Fatalf("detection doctor check = %#v", result.Checks)
	}
}
