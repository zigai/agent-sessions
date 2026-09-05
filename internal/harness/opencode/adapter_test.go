package opencode

import (
	"strings"
	"testing"

	"github.com/zigai/aht/internal/harness"
)

func TestPluginTemplateRendersCleanly(t *testing.T) {
	t.Parallel()

	h := New()
	plan := h.InstallPlan("/usr/local/bin/aht")
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least one install action")
	}
	action, ok := plan.Actions[0].(harness.RenderedFileAction)
	if !ok {
		t.Fatalf("expected harness.RenderedFileAction, got %T", plan.Actions[0])
	}
	rendered := action.Plan.Content
	if strings.TrimSpace(rendered) == "" {
		t.Fatal("rendered opencode template is empty")
	}
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		t.Fatalf("rendered opencode template contains unresolved placeholders:\n%s", rendered)
	}
}

func TestConfigDirOverride(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/opencode-config")
	t.Setenv("OPENCODE_CONFIG", "/tmp/ignored/config.json")
	if got := openCodeConfigDir(); got != "/tmp/opencode-config" {
		t.Fatalf("expected OPENCODE_CONFIG_DIR to win, got %q", got)
	}
}
