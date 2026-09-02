package manage_test

import (
	"slices"
	"testing"

	"github.com/zigai/aht/pkg/manage"
	"github.com/zigai/aht/pkg/registry"
)

func TestSupportedHarnessesReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	first := manage.SupportedHarnesses()
	if !slices.Contains(first, registry.HarnessCodex) {
		t.Fatalf("SupportedHarnesses() = %v, want codex", first)
	}
	first[0] = "mutated"

	if next := manage.SupportedHarnesses(); slices.Contains(next, registry.Harness("mutated")) {
		t.Fatalf("SupportedHarnesses() retained caller mutation: %v", next)
	}
}
