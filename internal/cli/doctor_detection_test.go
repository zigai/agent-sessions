package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorHandlesInvalidDetectionManifests(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     doctorStatus
	}{
		{name: "bundled override warns and falls back", manifest: "pi.toml", want: doctorWarning},
		{name: "local-only manifest fails", manifest: "agy.toml", want: doctorError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			detectionDir := filepath.Join(configHome, "aht", "detection")
			if err := os.MkdirAll(detectionDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(detectionDir, test.manifest), []byte("invalid ["), 0o600); err != nil {
				t.Fatal(err)
			}
			result := doctorResult{OK: true, Checks: nil, Capabilities: nil}
			(&application{}).addDetectionManifestCheck(&result)
			if len(result.Checks) != 1 || result.Checks[0].Name != "detection.manifests" || result.Checks[0].Status != test.want {
				t.Fatalf("detection doctor check = %#v, want status %q", result.Checks, test.want)
			}
		})
	}
}
