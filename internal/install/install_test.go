package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestInstallCodexMergesHooks(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())

	result, err := Run(Options{
		Harness:      registry.HarnessCodex,
		Binary:       "agent-sessions",
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected codex install to report changed")
	}

	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}

	var config map[string]any
	unmarshalErr := json.Unmarshal(data, &config)
	if unmarshalErr != nil {
		t.Fatalf("installed hooks are not valid JSON: %v", unmarshalErr)
	}

	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}
	_, hasSessionStart := hooks["SessionStart"]
	if !hasSessionStart {
		t.Fatal("expected SessionStart hook")
	}
	_, hasUserPrompt := hooks["UserPromptSubmit"]
	if !hasUserPrompt {
		t.Fatal("expected UserPromptSubmit hook")
	}
}

func TestInstallCodexReplacesManagedHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	path := filepath.Join(dir, "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating codex dir: %v", err)
	}
	oldConfig := `{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"old-agent-sessions report --harness codex --state idle --source codex-hook"}]}]}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatalf("writing old hooks: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessCodex,
		Binary:       "/usr/local/bin/agent-sessions",
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected codex install to replace old managed hook")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}
	if !strings.Contains(text, "--raw-stdin") || !strings.Contains(text, "--quiet") {
		t.Fatalf("expected stdin-aware quiet codex hook: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessCodex,
		Binary:       "/usr/local/bin/agent-sessions",
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second codex install to be idempotent")
	}
}

func TestInstallShimRequiresForceForForeignFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registry.StateDirEnv, dir)
	path := filepath.Join(dir, "shims", "opencode")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating shim dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("writing foreign shim: %v", err)
	}

	_, err := Run(Options{
		Harness:      registry.HarnessOpenCode,
		Binary:       "agent-sessions",
		TargetBinary: "/usr/bin/opencode",
		DryRun:       false,
		Force:        false,
		UseShim:      true,
	})
	if err == nil {
		t.Fatal("expected error for unmanaged shim")
	}
}

func TestInstallShimWritesManagedScript(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registry.StateDirEnv, dir)

	result, err := Run(Options{
		Harness:      registry.HarnessOpenCode,
		Binary:       "agent-sessions",
		TargetBinary: "/usr/bin/opencode",
		DryRun:       false,
		Force:        false,
		UseShim:      true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected shim install to report changed")
	}
	if !strings.Contains(result.Snippet, managedMarker) {
		t.Fatalf("expected managed marker in snippet: %q", result.Snippet)
	}
	if result.Path != filepath.Join(dir, "shims", "opencode") {
		t.Fatalf("unexpected path %q", result.Path)
	}
}

func TestInstallPiWritesExtension(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessPi,
		Binary:       "/usr/local/bin/agent-sessions",
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected pi install to report changed")
	}
	if result.Path != filepath.Join(dir, "extensions", piExtensionName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	if !strings.Contains(result.Snippet, `pi.on("agent_start"`) {
		t.Fatalf("expected agent_start hook in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `pi.on("before_agent_start"`) {
		t.Fatalf("expected before_agent_start hook in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, "AGENT_SESSIONS_INTEGRATION_ID=pi") {
		t.Fatalf("expected integration id in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `install-hooks", "pi"`) {
		t.Fatalf("expected self-refresh install command in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"--harness", "pi"`) {
		t.Fatalf("expected pi report command in snippet: %q", result.Snippet)
	}
}

func TestInstallOpenCodeWritesPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessOpenCode,
		Binary:       "/usr/local/bin/agent-sessions",
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected opencode install to report changed")
	}
	if result.Path != filepath.Join(dir, "opencode", "plugins", openCodePluginName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	if !strings.Contains(result.Snippet, "AGENT_SESSIONS_INTEGRATION_ID=opencode") {
		t.Fatalf("expected integration id in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `event: async ({ event }`) {
		t.Fatalf("expected native event handler in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"permission.asked"`) {
		t.Fatalf("expected permission event mapping in snippet: %q", result.Snippet)
	}
}

func TestRunAllInstallsEveryHarness(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv(registry.StateDirEnv, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	results, err := RunAll(Options{
		Harness:      "",
		Binary:       "agent-sessions",
		TargetBinary: "/usr/bin/opencode",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("RunAll returned error: %v", err)
	}
	if len(results) != len(AllHarnesses) {
		t.Fatalf("expected %d results, got %d", len(AllHarnesses), len(results))
	}

	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("unexpected result error for %s: %s", result.Harness, result.Error)
		}
		if result.Path == "" {
			t.Fatalf("expected path for %s", result.Harness)
		}
	}
}
