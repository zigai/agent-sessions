package manage_test

import (
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
