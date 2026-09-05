package opencode

import (
	"strings"
	"testing"
)

func TestPluginTemplateUsesCurrentModuleShape(t *testing.T) {
	t.Parallel()

	if !strings.Contains(openCodePluginTemplate, "async function AHTPlugin(ctx: PluginContext): Promise<PluginHooks>") {
		t.Fatal("expected per-context plugin factory")
	}
	if !strings.Contains(openCodePluginTemplate, `export default { id: "aht-state", server: AHTPlugin }`) {
		t.Fatal("expected native default plugin export")
	}
	if !strings.Contains(openCodePluginTemplate, `["properties", "status", "type"]`) {
		t.Fatal("expected nested session status handling")
	}
	if !strings.Contains(openCodePluginTemplate, `case "session.idle":`) {
		t.Fatal("expected deprecated idle event compatibility")
	}
	if !strings.Contains(openCodePluginTemplate, `child.once("error", warnReporting);`) {
		t.Fatal("expected asynchronous child error handling")
	}
}

func TestConfigDirOverride(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/opencode-config")
	t.Setenv("OPENCODE_CONFIG", "/tmp/ignored/config.json")
	if got := openCodeConfigDir(); got != "/tmp/opencode-config" {
		t.Fatalf("expected OPENCODE_CONFIG_DIR to win, got %q", got)
	}
}
