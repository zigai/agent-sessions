package harness_test

import (
	"slices"
	"testing"

	"github.com/zigai/aht/pkg/harness"
	"github.com/zigai/aht/pkg/registry"
)

func TestProcessNames(t *testing.T) {
	t.Parallel()

	names := harness.ProcessNames(registry.HarnessCodex)
	if !slices.Contains(names, "codex") {
		t.Errorf("ProcessNames(codex) = %v, want codex", names)
	}
}
