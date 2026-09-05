package kilo

import (
	"strings"
	"testing"
)

func TestPluginTemplateUsesCurrentModuleShape(t *testing.T) {
	t.Parallel()

	if !strings.Contains(kiloPluginTemplate, "async function AHTPlugin(ctx: PluginContext): Promise<PluginHooks>") {
		t.Fatal("expected per-context plugin factory")
	}
	if !strings.Contains(kiloPluginTemplate, `export default { id: "aht-state", server: AHTPlugin }`) {
		t.Fatal("expected native default plugin export")
	}
	if !strings.Contains(kiloPluginTemplate, `["properties", "status", "type"]`) {
		t.Fatal("expected nested session status handling")
	}
	if !strings.Contains(kiloPluginTemplate, `case "session.idle":`) {
		t.Fatal("expected deprecated idle event compatibility")
	}
	if !strings.Contains(kiloPluginTemplate, `child.once("error", warnReporting);`) {
		t.Fatal("expected asynchronous child error handling")
	}
}

func TestConfigDirOverride(t *testing.T) {
	t.Setenv("KILO_CONFIG_DIR", "/tmp/kilo-config")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/ignored-xdg")
	if got := kiloConfigDir(); got != "/tmp/kilo-config" {
		t.Fatalf("expected KILO_CONFIG_DIR to win, got %q", got)
	}
}
