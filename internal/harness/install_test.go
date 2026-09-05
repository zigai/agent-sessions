package harness_test

import (
	"strings"
	"testing"

	"github.com/zigai/aht/internal/harness"
)

func TestRenderScriptTemplateExpandsQueueAndLeavesNoPlaceholders(t *testing.T) {
	t.Parallel()

	sampleTemplate := "{{MANAGED_MARKER}}\nconst id = {{INTEGRATION_ID}};\nconst v = {{INTEGRATION_VERSION}};\nconst bin = {{BINARY}};\nconst src = {{SOURCE}};\n{{TYPESCRIPT_QUEUE}}\n"
	rendered := harness.RenderScriptTemplate(sampleTemplate, "test-integration", "/bin/test-aht", "test-source", 42)

	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		t.Fatalf("rendered template contains unresolved placeholders:\n%s", rendered)
	}
	for _, expected := range []string{
		harness.ManagedMarker,
		`const id = test-integration;`,
		`const v = 42;`,
		`const bin = "/bin/test-aht";`,
		`const src = "test-source";`,
		`child.once("error", warnReporting);`,
		`pendingCommands`,
		`enqueueCommand`,
		`runCommand`,
		`commandDrain`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("rendered template missing expected fragment %q", expected)
		}
	}
}
