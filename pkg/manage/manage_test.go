package manage_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zigai/aht/pkg/manage"
	"github.com/zigai/aht/pkg/registry"
)

func TestSupportedHarnesses(t *testing.T) {
	t.Parallel()

	harnesses := manage.SupportedHarnesses()
	for _, expected := range []registry.Harness{registry.HarnessClaude, registry.HarnessCodex, registry.HarnessOpenCode} {
		if !slices.Contains(harnesses, expected) {
			t.Errorf("SupportedHarnesses() missing %s", expected)
		}
	}
}

func TestManagerIntegrationStatus(t *testing.T) {
	t.Parallel()

	mgr := manage.New(manage.Config{
		Binary:    "aht",
		StorePath: filepath.Join(t.TempDir(), "sessions.json"),
	})
	status, err := mgr.IntegrationStatus(context.Background(), registry.HarnessCodex)
	if err != nil {
		t.Fatalf("IntegrationStatus(Codex) error = %v", err)
	}
	if status.Harness != registry.HarnessCodex {
		t.Errorf("status.Harness = %s, want %s", status.Harness, registry.HarnessCodex)
	}
}
