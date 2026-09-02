package harness_test

import (
	"slices"
	"testing"

	"github.com/zigai/aht/pkg/harness"
	"github.com/zigai/aht/pkg/registry"
)

func TestProcessNamesReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	first := harness.ProcessNames(registry.HarnessCodex)
	if len(first) == 0 {
		t.Fatal("ProcessNames(codex) returned no executable names")
	}
	original := slices.Clone(first)
	first[0] = "mutated"

	if next := harness.ProcessNames(registry.HarnessCodex); !slices.Equal(next, original) {
		t.Fatalf("ProcessNames(codex) after caller mutation = %v, want %v", next, original)
	}
}
