package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
)

const testInstallBinary = "/usr/local/bin/agent-sessions"

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
		Binary:       testInstallBinary,
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
		Binary:       testInstallBinary,
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

func TestInstallClaudeWritesHooks(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	result, err := Run(Options{
		Harness:      registry.HarnessClaude,
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
		t.Fatal("expected claude install to report changed")
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
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Notification", "Stop", "SessionEnd"} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
	}

	text := string(data)
	if !strings.Contains(text, "--raw-stdin") || !strings.Contains(text, "--quiet") {
		t.Fatalf("expected stdin-aware quiet claude hook: %s", text)
	}
	if !strings.Contains(text, "agent_sessions_integration=claude-hook") {
		t.Fatalf("expected managed claude hook marker: %s", text)
	}
	if !strings.Contains(text, managedMarker) {
		t.Fatalf("expected managed marker in claude hooks: %s", text)
	}
}

func TestInstallClaudeReplacesManagedHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating claude dir: %v", err)
	}
	oldConfig := `{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"old-agent-sessions report --harness claude --state idle --source claude-hook --attribute agent_sessions_integration=claude-hook","statusMessage":"agent-sessions managed integration"}]}]}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatalf("writing old hooks: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessClaude,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected claude install to replace old managed hook")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}
	if !strings.Contains(text, "install-hooks") || !strings.Contains(text, "claude") {
		t.Fatalf("expected self-refresh hook in snippet: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessClaude,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second claude install to be idempotent")
	}
}

func TestInstallCursorWritesHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Run(Options{
		Harness:      registry.HarnessCursor,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected cursor install to report changed")
	}
	if result.Path != filepath.Join(home, ".cursor", "hooks.json") {
		t.Fatalf("unexpected path %q", result.Path)
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
	if config["version"] != float64(1) {
		t.Fatalf("expected cursor hooks version 1, got %#v", config["version"])
	}

	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}
	for _, event := range []string{"sessionStart", "beforeSubmitPrompt", "beforeShellExecution", "afterShellExecution", "stop", "sessionEnd"} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
	}

	text := string(data)
	if !strings.Contains(text, "--raw-stdin-defaults-only") || strings.Contains(text, "--raw-stdin ") {
		t.Fatalf("expected defaults-only cursor hook commands: %s", text)
	}
	if !strings.Contains(text, "agent_sessions_integration=cursor-hook") {
		t.Fatalf("expected managed cursor hook marker: %s", text)
	}
	if !strings.Contains(text, "continue") || !strings.Contains(text, "permission") {
		t.Fatalf("expected non-blocking cursor hook responses: %s", text)
	}
}

func TestInstallCursorReplacesManagedHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating cursor dir: %v", err)
	}
	oldConfig := `{"version":1,"hooks":{"sessionStart":[{"command":"./user-hook.sh"},{"command":"old-agent-sessions report --harness cursor --state idle --source cursor-hook --attribute agent_sessions_integration=cursor-hook"}]}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatalf("writing old hooks: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessCursor,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected cursor install to replace old managed hook")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}
	if !strings.Contains(text, "./user-hook.sh") {
		t.Fatalf("expected user hook to be preserved: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessCursor,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second cursor install to be idempotent")
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
		Binary:       testInstallBinary,
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
		Binary:       testInstallBinary,
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

func TestInstallKiloWritesPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessKilo,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected kilo install to report changed")
	}
	if result.Path != filepath.Join(dir, "kilo", "plugin", kiloPluginName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	if !strings.Contains(result.Snippet, "AGENT_SESSIONS_INTEGRATION_ID=kilo") {
		t.Fatalf("expected integration id in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `export default { id: "agent-sessions-state", server: AgentSessionsPlugin };`) {
		t.Fatalf("expected kilo plugin default export in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `event: async ({ event }`) {
		t.Fatalf("expected native event handler in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"permission.asked"`) {
		t.Fatalf("expected permission event mapping in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `case "session.status":`) ||
		!strings.Contains(result.Snippet, `case "session.updated":`) {
		t.Fatalf("expected session status mappings in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `install-hooks", "kilo"`) {
		t.Fatalf("expected self-refresh install command in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"--harness", "kilo"`) {
		t.Fatalf("expected kilo report command in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"kilo_status"`) ||
		!strings.Contains(result.Snippet, `"agent_sessions_integration", "kilo-plugin"`) {
		t.Fatalf("expected kilo attributes in snippet: %q", result.Snippet)
	}
}

func TestInstallKiloReplacesManagedPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "kilo", "plugin", kiloPluginName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating kilo plugin dir: %v", err)
	}
	oldPlugin := `"agent-sessions managed integration";
const old = "old-agent-sessions";
`
	if err := os.WriteFile(path, []byte(oldPlugin), 0o600); err != nil {
		t.Fatalf("writing old plugin: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessKilo,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected kilo install to replace old managed plugin")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed plugin: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed plugin to be removed: %s", text)
	}
	if !strings.Contains(text, `install-hooks", "kilo"`) {
		t.Fatalf("expected self-refresh install command in plugin: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessKilo,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second kilo install to be idempotent")
	}
}

func TestInstallAgyWritesPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGY_CLI_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessAgy,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected agy install to report changed")
	}
	if result.Path != filepath.Join(dir, "plugins", agyPluginName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	if !strings.Contains(result.Snippet, "agy-hook") {
		t.Fatalf("expected agy hook command in snippet: %q", result.Snippet)
	}

	manifestData, err := os.ReadFile(filepath.Join(result.Path, "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("plugin manifest is not valid JSON: %v", err)
	}
	if manifest["name"] != agyPluginName {
		t.Fatalf("expected plugin name %q, got %#v", agyPluginName, manifest["name"])
	}

	hooksData, err := os.ReadFile(filepath.Join(result.Path, "hooks.json"))
	if err != nil {
		t.Fatalf("reading agy hooks: %v", err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(hooksData, &hooks); err != nil {
		t.Fatalf("agy hooks are not valid JSON: %v", err)
	}
	pluginHooks, ok := hooks[agyPluginName].(map[string]any)
	if !ok {
		t.Fatalf("expected %s hook namespace, got %#v", agyPluginName, hooks)
	}
	for _, event := range []string{"PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", "Stop"} {
		if _, ok := pluginHooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
	}

	marker, err := os.ReadFile(filepath.Join(result.Path, agyMarkerFileName))
	if err != nil {
		t.Fatalf("reading agy marker: %v", err)
	}
	if !strings.Contains(string(marker), managedMarker) {
		t.Fatalf("expected managed marker, got %q", marker)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessAgy,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second agy install to be idempotent")
	}
}

func TestInstallAgyRequiresForceForForeignPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGY_CLI_HOME", dir)
	pluginDir := filepath.Join(dir, "plugins", agyPluginName)
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatalf("creating agy plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"foreign"}`), 0o600); err != nil {
		t.Fatalf("writing foreign plugin manifest: %v", err)
	}

	_, err := Run(Options{
		Harness:      registry.HarnessAgy,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err == nil {
		t.Fatal("expected error for unmanaged agy plugin")
	}

	result, err := Run(Options{
		Harness:      registry.HarnessAgy,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        true,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("forced Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected forced agy install to report changed")
	}
}

func TestInstallKimiCodeWritesHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessKimiCode,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected kimi-code install to report changed")
	}
	if result.Path != filepath.Join(dir, "config.toml") {
		t.Fatalf("unexpected path %q", result.Path)
	}

	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	for _, event := range []string{
		"SessionStart",
		"UserPromptSubmit",
		"PermissionRequest",
		"PermissionResult",
		"Stop",
		"StopFailure",
		"Interrupt",
		"SessionEnd",
	} {
		if !strings.Contains(text, `event = "`+event+`"`) {
			t.Fatalf("expected %s hook in snippet: %s", event, text)
		}
	}
	for _, want := range []string{
		`matcher = "startup|resume"`,
		"--raw-stdin",
		"--quiet",
		"agent_sessions_integration=kimi-code-hook",
		managedMarker,
		"install-hooks kimi-code",
		"--state idle --event SessionStart",
		"--state running --event UserPromptSubmit",
		"--state waiting --event PermissionRequest",
		"--state running --event PermissionResult",
		"--state idle --event StopFailure",
		"--state idle --event Interrupt",
		"--state exited --event SessionEnd",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in snippet: %s", want, text)
		}
	}
	for _, forbidden := range []string{"statusMessage", "hooks =", "type ="} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unexpected unsupported Kimi hook field %q in snippet: %s", forbidden, text)
		}
	}
}

func TestInstallKimiCodeReplacesManagedBlockAndPreservesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating kimi-code dir: %v", err)
	}
	oldConfig := strings.Join([]string{
		`default_model = "kimi-code/kimi-for-coding"`,
		"",
		kimiCodeManagedStart,
		"[[hooks]]",
		`event = "SessionStart"`,
		`command = "old-agent-sessions report --harness kimi-code --source kimi-code-hook"`,
		kimiCodeManagedEnd,
		"",
		"[thinking]",
		`mode = "auto"`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatalf("writing old config: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessKimiCode,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected kimi-code install to replace old managed block")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	for _, want := range []string{`default_model = "kimi-code/kimi-for-coding"`, "[thinking]", `mode = "auto"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected preserved config %q in snippet: %s", want, text)
		}
	}
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessKimiCode,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second kimi-code install to be idempotent")
	}
}

func TestInstallKimiCodeDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessKimiCode,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       true,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected kimi-code dry-run to report changed")
	}
	if !strings.Contains(result.Snippet, `event = "SessionStart"`) {
		t.Fatalf("expected dry-run snippet to include Kimi hooks: %s", result.Snippet)
	}
	if _, err := os.Stat(result.Path); err == nil {
		t.Fatalf("expected dry-run not to write %s", result.Path)
	}
}

func TestInstallGrokWritesHooks(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())

	result, err := Run(Options{
		Harness:      registry.HarnessGrok,
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
		t.Fatal("expected grok install to report changed")
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
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd", "Notification"} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
	}

	text := string(data)
	if !strings.Contains(text, "--raw-stdin") || !strings.Contains(text, "--quiet") {
		t.Fatalf("expected stdin-aware quiet grok hook: %s", text)
	}
	if !strings.Contains(text, "agent_sessions_integration=grok-hook") {
		t.Fatalf("expected managed grok hook marker: %s", text)
	}
	if !strings.Contains(text, managedMarker) {
		t.Fatalf("expected managed marker in grok hooks: %s", text)
	}
}

func TestInstallGrokReplacesManagedHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	path := filepath.Join(dir, "hooks", grokHookFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating grok dir: %v", err)
	}
	oldConfig := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"old-agent-sessions report --harness grok --state idle --source grok-hook --attribute agent_sessions_integration=grok-hook","statusMessage":"agent-sessions managed integration"}]}]}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatalf("writing old hooks: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessGrok,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected grok install to replace old managed hook")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed hooks: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "old-agent-sessions") {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}
	if !strings.Contains(text, "install-hooks") || !strings.Contains(text, "grok") {
		t.Fatalf("expected self-refresh hook in snippet: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessGrok,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if second.Changed {
		t.Fatal("expected second grok install to be idempotent")
	}
}

func TestRunAllInstallsEveryHarness(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv(registry.StateDirEnv, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGY_CLI_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

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

func TestInstallersMatchHarnessCatalog(t *testing.T) {
	t.Parallel()

	catalog := make(map[registry.Harness]bool)
	for _, adapter := range harnesspkg.All() {
		catalog[adapter.ID] = true
		if !adapter.Installable {
			continue
		}
		if _, ok := installers[adapter.ID]; !ok {
			t.Fatalf("installable harness %q has no installer", adapter.ID)
		}
	}

	for harness := range installers {
		if !catalog[harness] {
			t.Fatalf("installer %q is not present in harness catalog", harness)
		}
	}

	for _, harness := range AllHarnesses {
		if _, ok := installers[harness]; !ok {
			t.Fatalf("AllHarnesses contains %q without installer", harness)
		}
	}
}
