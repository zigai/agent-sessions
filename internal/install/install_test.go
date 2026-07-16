package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	testInstallBinary     = "/usr/local/bin/agent-sessions"
	piExtensionName       = "agent-sessions-state.ts"
	ompExtensionName      = "agent-sessions-state.ts"
	openCodePluginName    = "agent-sessions-state.ts"
	kiloPluginName        = "agent-sessions-state.ts"
	agyPluginName         = "agent-sessions-state"
	agyMarkerFileName     = ".agent-sessions-managed"
	agyImportManifestName = "import_manifest.json"
	agyImportSource       = "antigravity"
	agyImportComponent    = "hooks"
	copilotHookFileName   = "agent-sessions.json"
	goosePluginName       = "agent-sessions-state"
	gooseMarkerFileName   = ".agent-sessions-managed"
	kimiCodeManagedStart  = "# BEGIN agent-sessions managed integration: kimi-code"
	kimiCodeManagedEnd    = "# END agent-sessions managed integration: kimi-code"
	grokHookFileName      = "agent-sessions-state.json"
)

func TestInstallCodexMergesHooks(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())

	result, err := Run(Options{
		Harness:      registry.HarnessCodex,
		Binary:       defaultBinary,
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
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("installed hooks are not valid JSON: %v", err)
	}

	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}
	_, hasSessionStart := hooks[hookEventSessionStart]
	if !hasSessionStart {
		t.Fatal("expected SessionStart hook")
	}
	_, hasUserPrompt := hooks["UserPromptSubmit"]
	if !hasUserPrompt {
		t.Fatal("expected UserPromptSubmit hook")
	}
	for _, event := range []string{"PostToolUse", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop"} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
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

	requireManagedReplacement(t, managedReplacementCase{
		Harness:              registry.HarnessCodex,
		Path:                 path,
		RemovedText:          "old-agent-sessions",
		RequiredText:         []string{"--raw-stdin", "--quiet"},
		FirstChangeMessage:   "expected codex install to replace old managed hook",
		SecondChangedMessage: "expected second codex install to be idempotent",
	})
}

func TestInstallClaudeWritesHooks(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	result, err := Run(Options{
		Harness:      registry.HarnessClaude,
		Binary:       defaultBinary,
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
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("installed hooks are not valid JSON: %v", err)
	}
	requireClaudeHookEvents(t, config)

	requireTextContains(t, string(data), []string{
		"--raw-stdin",
		"--queue",
		"--quiet",
		"agent_sessions_integration=claude-hook",
		managedMarker,
	})
}

func requireClaudeHookEvents(t *testing.T, config map[string]any) {
	t.Helper()
	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}
	for _, event := range []string{
		hookEventSessionStart,
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"PermissionRequest",
		"PermissionDenied",
		"Notification",
		"SubagentStart",
		"SubagentStop",
		"PreCompact",
		"PostCompact",
		hookEventStop,
		"StopFailure",
		"SessionEnd",
	} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("expected %s hook", event)
		}
	}
}

func requireTextContains(t *testing.T, text string, required []string) {
	t.Helper()
	for _, item := range required {
		if !strings.Contains(text, item) {
			t.Fatalf("expected installed hook to contain %q: %s", item, text)
		}
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

	requireManagedReplacement(t, managedReplacementCase{
		Harness:              registry.HarnessClaude,
		Path:                 path,
		RemovedText:          "old-agent-sessions",
		RequiredText:         []string{"--raw-stdin", "agent_sessions_integration_version=3"},
		FirstChangeMessage:   "expected claude install to replace old managed hook",
		SecondChangedMessage: "expected second claude install to be idempotent",
	})
}

func TestInstallClaudeRepairsManagedHookMatcher(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	first, err := Run(Options{
		Harness:      registry.HarnessClaude,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("initial Run returned error: %v", err)
	}

	config := decodeTestJSONObject(t, readTestFile(t, first.Path, "reading claude hooks"), "claude hooks")
	hooks := requireTestHooks(t, config)
	notificationGroups, ok := hooks["Notification"].([]any)
	if !ok || len(notificationGroups) == 0 {
		t.Fatalf("expected Notification hook groups, got %#v", hooks["Notification"])
	}
	group, ok := notificationGroups[0].(map[string]any)
	if !ok {
		t.Fatalf("expected Notification hook group object, got %#v", notificationGroups[0])
	}
	group["matcher"] = "*"

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("encoding modified hooks: %v", err)
	}
	if err := os.WriteFile(first.Path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("writing modified hooks: %v", err)
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
		t.Fatalf("repair Run returned error: %v", err)
	}
	if !second.Changed {
		t.Fatal("expected reinstall to repair stale managed matcher")
	}

	text := string(readTestFile(t, first.Path, "reading repaired hooks"))
	if !strings.Contains(text, `"matcher": "permission_prompt"`) || strings.Contains(text, `"matcher": "*"`) {
		t.Fatalf("expected repaired notification matcher, got %s", text)
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

	data := readTestFile(t, result.Path, "reading installed hooks")
	config := decodeTestJSONObject(t, data, "installed hooks")
	if config["version"] != float64(1) {
		t.Fatalf("expected cursor hooks version 1, got %#v", config["version"])
	}

	hooks := requireTestHooks(t, config)
	requireTestHookEvents(t, hooks, []string{
		"sessionStart",
		"beforeSubmitPrompt",
		"stop",
		"sessionEnd",
	})

	text := string(data)
	requireTextContainsAll(t, text, []string{
		"--raw-stdin-defaults-only",
		"agent_sessions_integration=cursor-hook",
		"continue",
	}, "cursor hooks")
	if strings.Contains(text, "--raw-stdin ") {
		t.Fatalf("expected defaults-only cursor hook commands: %s", text)
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

func TestInstallCopilotWritesHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessCopilot,
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
		t.Fatal("expected copilot install to report changed")
	}
	if result.Path != filepath.Join(dir, "hooks", copilotHookFileName) {
		t.Fatalf("unexpected path %q", result.Path)
	}

	config := decodeTestJSONObject(t, readTestFile(t, result.Path, "reading copilot hooks"), "copilot hooks")
	if config["version"] != float64(1) {
		t.Fatalf("expected Copilot hooks version 1, got %#v", config["version"])
	}
	hooks := requireTestHooks(t, config)
	requireTestHookEvents(t, hooks, []string{
		"sessionStart",
		"userPromptSubmitted",
		"preToolUse",
		"permissionRequest",
		"postToolUse",
		"postToolUseFailure",
		"agentStop",
		"sessionEnd",
	})
	text := string(readTestFile(t, result.Path, "reading copilot hooks text"))
	requireTextContainsAll(t, text, []string{
		"--raw-stdin-defaults-only",
		"agent_sessions_integration=copilot-hook",
		"copilot_hook_event=preToolUse",
		managedMarker,
		"|| true",
	}, "copilot hooks")
}

func TestInstallClineWritesHookFiles(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	t.Setenv("CLINE_HOOKS_DIR", hooksDir)

	result, err := Run(Options{
		Harness:      registry.HarnessCline,
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
		t.Fatal("expected cline install to report changed")
	}
	if result.Path != hooksDir {
		t.Fatalf("unexpected path %q", result.Path)
	}

	for _, name := range []string{
		"TaskStart.sh",
		"TaskResume.sh",
		"UserPromptSubmit.sh",
		"PreToolUse.sh",
		"PostToolUse.sh",
		"TaskComplete.sh",
		"TaskCancel.sh",
		"TaskError.sh",
		"PreCompact.sh",
		"SessionShutdown.sh",
	} {
		text := string(readTestFile(t, filepath.Join(hooksDir, name), "reading cline hook "+name))
		requireTextContainsAll(t, text, []string{
			managedMarker,
			"--raw-stdin-defaults-only",
			"agent_sessions_integration=cline-hook",
			"printf '%s\\n' '{}'",
		}, "cline hook "+name)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessCline,
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
		t.Fatal("expected second cline install to be idempotent")
	}
}

func TestInstallClineRequiresForceForForeignHook(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	t.Setenv("CLINE_HOOKS_DIR", hooksDir)
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("creating cline hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "TaskStart.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("writing foreign cline hook: %v", err)
	}

	_, err := Run(Options{
		Harness:      registry.HarnessCline,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err == nil {
		t.Fatal("expected error for unmanaged cline hook")
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
		Binary:       defaultBinary,
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
		Binary:       defaultBinary,
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

func TestInstallShimSupportsHarnessesMissingExitHooks(t *testing.T) {
	for _, tc := range []struct {
		name         string
		harness      registry.Harness
		targetBinary string
	}{
		{name: "codex", harness: registry.HarnessCodex, targetBinary: "/usr/bin/codex"},
		{name: "agy", harness: registry.HarnessAgy, targetBinary: "/usr/bin/agy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(registry.StateDirEnv, dir)

			result, err := Run(Options{
				Harness:      tc.harness,
				Binary:       defaultBinary,
				TargetBinary: tc.targetBinary,
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
			if result.Path != filepath.Join(dir, "shims", string(tc.harness)) {
				t.Fatalf("unexpected path %q", result.Path)
			}
			requireTextContainsAll(t, result.Snippet, []string{
				managedMarker,
				"harness_bin=" + tc.targetBinary,
				"report " + string(tc.harness) + " --presence live --evidence process --pid \"$$\"",
				"report " + string(tc.harness) + " --presence gone --evidence process --pid \"$$\"",
			}, "shim script")
		})
	}
}

func TestInstallShimResolvesTargetOutsideManagedShimDir(t *testing.T) {
	dir := t.TempDir()
	realDir := t.TempDir()
	t.Setenv(registry.StateDirEnv, dir)
	t.Setenv("PATH", filepath.Join(dir, "shims")+string(os.PathListSeparator)+realDir)

	shimPath := filepath.Join(dir, "shims", "opencode")
	if err := os.MkdirAll(filepath.Dir(shimPath), 0o700); err != nil {
		t.Fatalf("creating shim dir: %v", err)
	}
	if err := writeExecutableTestFile(shimPath, []byte("#!/bin/sh\n# "+managedMarker+"\n")); err != nil {
		t.Fatalf("writing existing shim: %v", err)
	}
	realPath := filepath.Join(realDir, "opencode")
	if err := writeExecutableTestFile(realPath, []byte("#!/bin/sh\n")); err != nil {
		t.Fatalf("writing real harness binary: %v", err)
	}

	result, err := Run(Options{
		Harness:      registry.HarnessOpenCode,
		Binary:       defaultBinary,
		TargetBinary: "",
		DryRun:       true,
		Force:        false,
		UseShim:      true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(result.Snippet, "harness_bin="+realPath) {
		t.Fatalf("expected shim to target real binary %q, got snippet: %s", realPath, result.Snippet)
	}
	if strings.Contains(result.Snippet, "harness_bin="+shimPath) {
		t.Fatalf("shim targets itself: %s", result.Snippet)
	}
}

func TestInstallShimRejectsManagedShimTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registry.StateDirEnv, dir)
	shimPath := filepath.Join(dir, "shims", "opencode")

	_, err := Run(Options{
		Harness:      registry.HarnessOpenCode,
		Binary:       defaultBinary,
		TargetBinary: shimPath,
		DryRun:       true,
		Force:        false,
		UseShim:      true,
	})
	if !errors.Is(err, errRecursiveShimTarget) {
		t.Fatalf("expected errRecursiveShimTarget, got %v", err)
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
	if !strings.Contains(result.Snippet, `"report", "pi"`) {
		t.Fatalf("expected pi report command in snippet: %q", result.Snippet)
	}
	if !strings.Contains(result.Snippet, `"--observed-at", observedAt`) {
		t.Fatalf("expected pi observed timestamp in snippet: %q", result.Snippet)
	}
}

func TestInstallOmpWritesExtension(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)

	result, err := Run(Options{
		Harness:      registry.HarnessOmp,
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
		t.Fatal("expected oh-my-pi install to report changed")
	}
	if result.Path != filepath.Join(dir, "extensions", ompExtensionName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	requireTextContainsAll(t, result.Snippet, []string{
		"AGENT_SESSIONS_INTEGRATION_ID=omp",
		`pi.on("session_start"`,
		`pi.on("tool_approval_requested"`,
		`pi.on("tool_approval_resolved"`,
		`pi.on("session_stop"`,
		`pi.on("session_shutdown"`,
		`report("idle", "live"`,
		`report("waiting"`,
		`report("running"`,
		`report(undefined, "gone"`,
		`"--resume-command", item`,
		`"--session-path", currentSessionPath`,
		`"--cwd", currentCwd`,
		`"report", "omp"`,
		"AGENT_SESSIONS_INTEGRATION_VERSION=3",
	}, "oh-my-pi extension")
}

func TestInstallOmpUsesProfileAgentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", ".omp")
	t.Setenv("OMP_PROFILE", "work")
	t.Setenv("PI_PROFILE", "")

	result, err := Run(Options{
		Harness:      registry.HarnessOmp,
		Binary:       testInstallBinary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantPath := filepath.Join(home, ".omp", "profiles", "work", "agent", "extensions", ompExtensionName)
	if result.Path != wantPath {
		t.Fatalf("unexpected profile path %q, want %q", result.Path, wantPath)
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
	if !strings.Contains(result.Snippet, `"--observed-at", observedAt`) {
		t.Fatalf("expected opencode observed timestamp in snippet: %q", result.Snippet)
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
	requireTextContainsAll(t, result.Snippet, []string{
		"AGENT_SESSIONS_INTEGRATION_ID=kilo",
		`export default { id: "agent-sessions-state", server: AgentSessionsPlugin };`,
		`event: async ({ event }`,
		`"permission.asked"`,
		`"AGENT_SESSIONS_INTEGRATION_VERSION=3"`,
		`"--observed-at", observedAt`,
		`"kilo_status"`,
		`"agent_sessions_integration", source`,
	}, "kilo snippet")
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
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	if result.Path != filepath.Join(home, ".gemini", "antigravity-cli", "plugins", agyPluginName) {
		t.Fatalf("unexpected path %q", result.Path)
	}
	requireTextContainsAll(t, result.Snippet, []string{"hook agy"}, "agy snippet")
	requireAgyPluginManifest(t, result.Path)
	requireAgyPluginHooks(t, result.Path)
	requireAgyPluginMarker(t, result.Path)
	requireAgyImportManifest(t, filepath.Join(home, ".gemini", "antigravity-cli", agyImportManifestName))

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
	home := t.TempDir()
	t.Setenv("HOME", home)
	pluginDir := filepath.Join(home, ".gemini", "antigravity-cli", "plugins", agyPluginName)
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

func TestInstallGooseWritesPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Run(Options{
		Harness:      registry.HarnessGoose,
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
		t.Fatal("expected goose install to report changed")
	}
	if result.Path != filepath.Join(home, ".agents", "plugins", goosePluginName) {
		t.Fatalf("unexpected path %q", result.Path)
	}

	requireGoosePluginManifest(t, result.Path)
	requireGoosePluginHooks(t, result.Path)
	requireGoosePluginScript(t, result.Path)
	requireGoosePluginMarker(t, result.Path)

	second, err := Run(Options{
		Harness:      registry.HarnessGoose,
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
		t.Fatal("expected second goose install to be idempotent")
	}
}

func TestInstallDroidWritesHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Run(Options{
		Harness:      registry.HarnessDroid,
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
		t.Fatal("expected droid install to report changed")
	}
	if result.Path != filepath.Join(home, ".factory", "hooks.json") {
		t.Fatalf("unexpected path %q", result.Path)
	}

	data := readTestFile(t, result.Path, "reading droid hooks")
	config := decodeTestJSONObject(t, data, "droid hooks")
	hooks := requireTestHooks(t, config)
	requireTestHookEvents(t, hooks, []string{
		hookEventSessionStart,
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"Notification",
		hookEventStop,
		"SubagentStop",
		"PreCompact",
		"SessionEnd",
	})
	text := string(data)
	requireTextContainsAll(t, text, []string{
		"--raw-stdin-defaults-only",
		"agent_sessions_integration=droid-hook",
		"agent_sessions_integration_version=3",
	}, "droid hooks")
	if strings.Contains(text, "statusMessage") {
		t.Fatalf("expected Droid hooks not to include unsupported statusMessage field: %s", text)
	}

	second, err := Run(Options{
		Harness:      registry.HarnessDroid,
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
		t.Fatal("expected second droid install to be idempotent")
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
		hookEventSessionStart,
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"PermissionRequest",
		"PermissionResult",
		hookEventStop,
		"StopFailure",
		"Interrupt",
		"SubagentStart",
		"SubagentStop",
		"PreCompact",
		"PostCompact",
		"Notification",
		"SessionEnd",
	} {
		if !strings.Contains(text, `event = "`+event+`"`) {
			t.Fatalf("expected %s hook in snippet: %s", event, text)
		}
	}
	for _, want := range []string{
		`matcher = "startup|resume"`,
		`matcher = "task\\.completed"`,
		`event = "SessionEnd"` + "\nmatcher = \"exit\"",
		"--raw-stdin",
		"--quiet",
		"agent_sessions_integration=kimi-code-hook",
		"agent_sessions_integration_version=3",
		managedMarker,
		"--activity idle --event SessionStart",
		"--activity running --event UserPromptSubmit",
		"--activity running --event PreToolUse",
		"--activity waiting --event PermissionRequest",
		"--activity running --event PermissionResult",
		"--activity idle --event Notification",
		"--activity idle --event StopFailure",
		"--activity idle --event Interrupt",
		"--presence gone --event SessionEnd",
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
		`event = "` + hookEventSessionStart + `"`,
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
	if !strings.Contains(result.Snippet, `event = "`+hookEventSessionStart+`"`) {
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
		Binary:       defaultBinary,
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
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("installed hooks are not valid JSON: %v", err)
	}

	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}
	for _, event := range []string{
		hookEventSessionStart,
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"PermissionDenied",
		"SubagentStart",
		"SubagentStop",
		"PreCompact",
		"PostCompact",
		hookEventStop,
		"StopFailure",
		"SessionEnd",
	} {
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
		Binary:       defaultBinary,
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

func TestInstallPlansMatchHarnessCatalog(t *testing.T) {
	t.Parallel()

	for _, adapter := range harnesspkg.All() {
		if _, ok := adapter.(harnesspkg.Installable); !ok {
			t.Fatalf("harness %q has no install plan", adapter.Definition().ID)
		}
	}

	for _, harness := range AllHarnesses {
		adapter, ok := harnesspkg.Find(harness)
		if !ok {
			t.Fatalf("AllHarnesses contains unknown harness %q", harness)
		}
		if _, installable := adapter.(harnesspkg.Installable); !installable {
			t.Fatalf("AllHarnesses contains %q without install plan", harness)
		}
	}
}

type managedReplacementCase struct {
	Harness              registry.Harness
	Path                 string
	RemovedText          string
	RequiredText         []string
	FirstChangeMessage   string
	SecondChangedMessage string
}

func requireManagedReplacement(t *testing.T, test managedReplacementCase) {
	t.Helper()

	result, err := Run(Options{
		Harness:      test.Harness,
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
		t.Fatal(test.FirstChangeMessage)
	}

	text := string(readTestFile(t, test.Path, "reading installed hooks"))
	if strings.Contains(text, test.RemovedText) {
		t.Fatalf("expected old managed hook to be removed: %s", text)
	}
	requireTextContainsAll(t, text, test.RequiredText, "installed hooks")

	second, err := Run(Options{
		Harness:      test.Harness,
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
		t.Fatal(test.SecondChangedMessage)
	}
}

func readTestFile(t *testing.T, path string, context string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}

	return data
}

func decodeTestJSONObject(t *testing.T, data []byte, context string) map[string]any {
	t.Helper()

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON for %s: %v", context, err)
	}

	return config
}

func requireTestHooks(t *testing.T, config map[string]any) map[string]any {
	t.Helper()

	hooks, hooksOK := config["hooks"].(map[string]any)
	if !hooksOK {
		t.Fatal("expected hooks object")
	}

	return hooks
}

func requireTestHookEvents(t *testing.T, hooks map[string]any, events []string) {
	t.Helper()

	for _, event := range events {
		if _, hasEvent := hooks[event]; !hasEvent {
			t.Fatalf("expected %s hook", event)
		}
	}
}

func requireTextContainsAll(t *testing.T, text string, values []string, context string) {
	t.Helper()

	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Fatalf("expected %q in %s: %s", value, context, text)
		}
	}
}

func requireAgyPluginManifest(t *testing.T, dir string) {
	t.Helper()

	manifestData := readTestFile(t, filepath.Join(dir, "plugin.json"), "reading plugin manifest")
	manifest := decodeTestJSONObject(t, manifestData, "plugin manifest")
	if manifest["name"] != agyPluginName {
		t.Fatalf("expected plugin name %q, got %#v", agyPluginName, manifest["name"])
	}
}

func requireAgyPluginHooks(t *testing.T, dir string) {
	t.Helper()

	hooksData := readTestFile(t, filepath.Join(dir, "hooks.json"), "reading agy hooks")
	if !strings.Contains(string(hooksData), testInstallBinary+" --json hook agy") {
		t.Fatalf("expected agy hooks to request protocol JSON explicitly: %s", hooksData)
	}
	hooks := decodeTestJSONObject(t, hooksData, "agy hooks")
	pluginHooks, hooksOK := hooks[agyPluginName].(map[string]any)
	if !hooksOK {
		t.Fatalf("expected %s hook namespace, got %#v", agyPluginName, hooks)
	}
	requireTestHookEvents(t, pluginHooks, []string{"PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", hookEventStop})
}

func requireAgyPluginMarker(t *testing.T, dir string) {
	t.Helper()

	marker := readTestFile(t, filepath.Join(dir, agyMarkerFileName), "reading agy marker")
	if !strings.Contains(string(marker), managedMarker) {
		t.Fatalf("expected managed marker, got %q", marker)
	}
	if !strings.Contains(string(marker), "AGENT_SESSIONS_INTEGRATION_VERSION=4") {
		t.Fatalf("expected agy integration version 4 marker, got %q", marker)
	}
}

func requireAgyImportManifest(t *testing.T, path string) {
	t.Helper()

	data := readTestFile(t, path, "reading agy import manifest")
	manifest := decodeTestJSONObject(t, data, "agy import manifest")
	imports, importsOK := manifest["imports"].([]any)
	if !importsOK {
		t.Fatalf("expected agy imports list, got %#v", manifest)
	}

	for _, importValue := range imports {
		importItem, importOK := importValue.(map[string]any)
		if !importOK || importItem["name"] != agyPluginName {
			continue
		}
		if importItem["source"] != agyImportSource {
			t.Fatalf("expected agy import source %q, got %#v", agyImportSource, importItem["source"])
		}
		components, componentsOK := importItem["components"].([]any)
		if !componentsOK {
			t.Fatalf("expected agy import components, got %#v", importItem["components"])
		}
		for _, component := range components {
			if component == agyImportComponent {
				return
			}
		}
		t.Fatalf("expected agy import component %q, got %#v", agyImportComponent, components)
	}

	t.Fatalf("expected agy import for %q, got %#v", agyPluginName, imports)
}

func requireGoosePluginManifest(t *testing.T, dir string) {
	t.Helper()

	manifestData := readTestFile(t, filepath.Join(dir, "plugin.json"), "reading goose plugin manifest")
	manifest := decodeTestJSONObject(t, manifestData, "goose plugin manifest")
	if manifest["name"] != goosePluginName {
		t.Fatalf("expected plugin name %q, got %#v", goosePluginName, manifest["name"])
	}
	if manifest["description"] != managedMarker {
		t.Fatalf("expected managed marker description, got %#v", manifest["description"])
	}
}

func requireGoosePluginHooks(t *testing.T, dir string) {
	t.Helper()

	hooksData := readTestFile(t, filepath.Join(dir, "hooks", "hooks.json"), "reading goose hooks")
	hooksConfig := decodeTestJSONObject(t, hooksData, "goose hooks")
	hooks := requireTestHooks(t, hooksConfig)
	requireTestHookEvents(t, hooks, []string{
		hookEventSessionStart,
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"BeforeReadFile",
		"AfterFileEdit",
		"BeforeShellExecution",
		"AfterShellExecution",
		hookEventStop,
		"SessionEnd",
	})

	text := string(hooksData)
	requireTextContainsAll(t, text, []string{
		"${PLUGIN_ROOT}/scripts/report.sh",
	}, "goose hooks")
}

func requireGoosePluginScript(t *testing.T, dir string) {
	t.Helper()

	text := string(readTestFile(t, filepath.Join(dir, "scripts", "report.sh"), "reading goose report script"))
	requireTextContainsAll(t, text, []string{
		managedMarker,
		"--raw-stdin-defaults-only",
		"agent_sessions_integration=goose-hook",
		"agent_sessions_integration_version=3",
		`--presence "$transition"`,
		`--activity "$transition"`,
		`--event "$event"`,
	}, "goose report script")
}

func requireGoosePluginMarker(t *testing.T, dir string) {
	t.Helper()

	marker := readTestFile(t, filepath.Join(dir, gooseMarkerFileName), "reading goose marker")
	if !strings.Contains(string(marker), managedMarker) {
		t.Fatalf("expected managed marker, got %q", marker)
	}
}

func writeExecutableTestFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing executable test file: %w", err)
	}

	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("marking executable test file executable: %w", err)
	}

	return nil
}
