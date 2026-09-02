//go:build integration

package install

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	harnesspkg "github.com/zigai/aht/internal/harness"
	harnesscatalog "github.com/zigai/aht/internal/harness/catalog"
	"github.com/zigai/aht/pkg/registry"
	"go.yaml.in/yaml/v3"
)

const generatedRuntimeSensitiveSentinel = "AHT_PHASE3_SENSITIVE_SENTINEL"

type generatedArtifact struct {
	harness registry.Harness
	path    string
	content string
}

func TestGeneratedArtifactsParse(t *testing.T) {
	requireRuntimeTool(t, "node")
	requireRuntimeTool(t, "python3")
	requireRuntimeTool(t, "sh")

	artifacts := collectGeneratedArtifacts(t, captureBinary(t))
	seenHarnesses := make(map[registry.Harness]bool)
	for _, artifact := range artifacts {
		artifact := artifact
		t.Run(string(artifact.harness)+"/"+strings.ReplaceAll(artifact.path, "/", "_"), func(t *testing.T) {
			validateGeneratedArtifact(t, artifact)
		})
		seenHarnesses[artifact.harness] = true
	}
	for _, harness := range AllHarnesses() {
		if !seenHarnesses[harness] {
			t.Fatalf("generated artifact validation missed harness %q", harness)
		}
	}
}

func TestGeneratedRuntimeFamilies(t *testing.T) {
	requireRuntimeTool(t, "node")
	requireRuntimeTool(t, "python3")
	requireRuntimeTool(t, "sh")

	capture := captureBinary(t)
	t.Setenv("AHT_CAPTURE", capture.path)

	t.Run("command-hook", func(t *testing.T) {
		command := generatedCommandHook(t, registry.HarnessClaude)
		runGeneratedCommand(t, exec.Command("sh", "-c", command), `{"session_id":"command-session","prompt":"`+generatedRuntimeSensitiveSentinel+`"}`)
		requireCapturedArguments(t, capture.path, "report", "claude", "--activity", "idle")
	})

	t.Run("goose-shell-wrapper", func(t *testing.T) {
		script := writeRuntimeArtifact(t, "report.sh", generatedArtifactContent(t, registry.HarnessGoose, "scripts/report.sh"))
		runGeneratedCommand(t, exec.Command("sh", script, "running", "UserPromptSubmit"), `{"session_id":"goose-session","prompt":"`+generatedRuntimeSensitiveSentinel+`"}`)
		requireCapturedArguments(t, capture.path, "report", "goose", "--activity", "running")
	})

	t.Run("cline-plugin", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessCline, "index.js")
		runNodeRuntime(t, "index.js", module, `
import plugin from "./index.js";
const hooks = plugin.hooks;
plugin.setup({}, {session: {sessionId: "cline-session"}, workspaceInfo: {rootPath: "/tmp/project"}});
hooks.afterRun({snapshot: {status: "failed", prompt: "`+generatedRuntimeSensitiveSentinel+`"}, result: {status: "failed"}});
await new Promise((resolve) => setTimeout(resolve, 100));
`, nil)
		requireCapturedArguments(t, capture.path, "report", "cline", "--activity", "failed")
	})

	t.Run("openclaw-plugin", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessOpenClaw, "index.js")
		extra := map[string]string{
			"node_modules/openclaw/package.json": `{"name":"openclaw","type":"module","exports":{"./plugin-sdk/plugin-entry":"./plugin-entry.js"}}`,
			"node_modules/openclaw/plugin-entry.js": `export function definePluginEntry(value) { return value; }`,
		}
		runNodeRuntime(t, "index.js", module, `
import plugin from "./index.js";
const hooks = new Map();
plugin.register({on: (name, callback) => hooks.set(name, callback)});
hooks.get("agent_end")({success: false, reason: "error", prompt: "`+generatedRuntimeSensitiveSentinel+`"}, {sessionId: "openclaw-session", workspaceDir: "/tmp/project"});
await new Promise((resolve) => setTimeout(resolve, 100));
`, extra)
		requireCapturedArguments(t, capture.path, "report", "openclaw", "--activity", "failed")
	})

	t.Run("hermes-plugin", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessHermes, "__init__.py")
		dir := t.TempDir()
		modulePath := filepath.Join(dir, "aht_state.py")
		writeTestFile(t, modulePath, module, 0o600)
		driver := `
import importlib.util
import sys
spec = importlib.util.spec_from_file_location("aht_state", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
class Context:
    def __init__(self):
        self.hooks = {}
    def register_hook(self, name, callback):
        self.hooks[name] = callback
ctx = Context()
module.register(ctx)
ctx.hooks["on_session_end"](session_id="hermes-session", completed=False, prompt="` + generatedRuntimeSensitiveSentinel + `")
`
		driverPath := filepath.Join(dir, "driver.py")
		writeTestFile(t, driverPath, driver, 0o600)
		runGeneratedCommand(t, exec.Command("python3", driverPath, modulePath), "")
		requireCapturedArguments(t, capture.path, "report", "hermes", "--activity", "failed")
	})

	t.Run("pi-extension", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessPi, "aht-state.ts")
		runNodeRuntime(t, "extension.ts", module, `
import extension from "./extension.ts";
const hooks = new Map();
extension({on: (name, callback) => hooks.set(name, callback)});
const ctx = {cwd: "/tmp/project", sessionManager: {getSessionId: () => "pi-session", getSessionFile: () => "/tmp/pi.jsonl"}};
hooks.get("agent_end")({type: "agent_end", status: "failed", prompt: "`+generatedRuntimeSensitiveSentinel+`"}, ctx);
await new Promise((resolve) => setTimeout(resolve, 100));
`, nil)
		requireCapturedArguments(t, capture.path, "report", "pi", "--activity", "failed")
	})

	t.Run("omp-extension", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessOmp, "aht-state.ts")
		runNodeRuntime(t, "extension.ts", module, `
import extension from "./extension.ts";
const hooks = new Map();
extension({on: (name, callback) => hooks.set(name, callback)});
const ctx = {hasUI: true, cwd: "/tmp/project", sessionManager: {getSessionId: () => "omp-session", getSessionFile: () => "/tmp/omp.jsonl", getBranch: () => []}};
hooks.get("session_start")({type: "session_start"}, ctx);
hooks.get("agent_error")({type: "agent_error", error: "fatal", prompt: "`+generatedRuntimeSensitiveSentinel+`"}, ctx);
await new Promise((resolve) => setTimeout(resolve, 200));
`, nil)
		requireCapturedArguments(t, capture.path, "report", "omp", "--activity", "failed")
	})

	t.Run("opencode-kilo-plugin-family", func(t *testing.T) {
		module := generatedArtifactContent(t, registry.HarnessOpenCode, "aht-state.ts")
		runNodeRuntime(t, "plugin.ts", module, `
import plugin from "./plugin.ts";
const runtime = await plugin.server({directory: "/tmp/project", worktree: "/tmp/project"});
await runtime.event({event: {type: "session.error", sessionID: "opencode-session", prompt: "`+generatedRuntimeSensitiveSentinel+`"}});
await new Promise((resolve) => setTimeout(resolve, 100));
`, nil)
		requireCapturedArguments(t, capture.path, "report", "opencode", "--activity", "failed")
	})

	data, err := os.ReadFile(capture.path)
	if err != nil {
		t.Fatalf("read runtime capture: %v", err)
	}
	if strings.Contains(string(data), generatedRuntimeSensitiveSentinel) {
		t.Fatalf("generated runtime leaked sensitive payload into report arguments:\n%s", data)
	}
}

func collectGeneratedArtifacts(t *testing.T, binary captureExecutable) []generatedArtifact {
	t.Helper()
	artifacts := make([]generatedArtifact, 0)
	for _, adapter := range harnesscatalog.All() {
		installer, ok := adapter.(harnesspkg.Installable)
		if !ok {
			continue
		}
		harness := adapter.Definition().ID
		for _, action := range installer.InstallPlan(binary.command).Actions {
			switch plan := action.(type) {
			case harnesspkg.JSONCommandHooksAction:
				config := make(map[string]any)
				applyJSONCommandHooks(harness, plan.Plan)(config)
				artifacts = append(artifacts, generatedJSONArtifact(t, harness, plan.Plan.Path, config))
			case harnesspkg.CursorJSONHooksAction:
				config := make(map[string]any)
				applyCursorJSONHooks(harness, plan.Plan)(config)
				artifacts = append(artifacts, generatedJSONArtifact(t, harness, plan.Plan.Path, config))
			case harnesspkg.ManagedTextBlockAction:
				artifacts = append(artifacts, generatedArtifact{harness: harness, path: plan.Plan.Path, content: plan.Plan.Block})
			case harnesspkg.RenderedFileAction:
				content, err := renderInstallContent(plan.Plan.Content, plan.Plan.JSONContent)
				if err != nil {
					t.Fatalf("render %s artifact: %v", harness, err)
				}
				artifacts = append(artifacts, generatedArtifact{harness: harness, path: plan.Plan.Path, content: content})
			case harnesspkg.RenderedFilesAction:
				for name, content := range mustRenderFiles(t, plan.Plan.Files) {
					artifacts = append(artifacts, generatedArtifact{harness: harness, path: filepath.Join(plan.Plan.Dir, name), content: content})
				}
			case harnesspkg.PluginDirectoryAction:
				for _, file := range plan.Plan.Files {
					content, err := renderInstallContent(file.Content, file.JSONContent)
					if err != nil {
						t.Fatalf("render %s plugin artifact %s: %v", harness, file.Name, err)
					}
					artifacts = append(artifacts, generatedArtifact{harness: harness, path: file.Name, content: content})
				}
			case harnesspkg.ShimAction:
				artifacts = append(artifacts, generatedArtifact{harness: harness, path: string(harness) + ".sh", content: shimScript(binary.command, string(harness), "/usr/bin/true", harnesscatalog.IntegrationVersionFor(harness))})
			default:
				t.Fatalf("unvalidated install action for %s: %T", harness, action)
			}
		}
	}
	return artifacts
}

func mustRenderFiles(t *testing.T, specs []harnesspkg.RenderedFileInstallSpec) map[string]string {
	t.Helper()
	files, err := renderRenderedFiles(specs)
	if err != nil {
		t.Fatalf("render generated files: %v", err)
	}
	return files
}

func generatedJSONArtifact(t *testing.T, harness registry.Harness, path string, value any) generatedArtifact {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s generated JSON: %v", harness, err)
	}
	return generatedArtifact{harness: harness, path: path, content: string(content)}
}

func validateGeneratedArtifact(t *testing.T, artifact generatedArtifact) {
	t.Helper()
	path := strings.ToLower(artifact.path)
	switch {
	case strings.HasSuffix(path, ".json"):
		if !json.Valid([]byte(artifact.content)) {
			t.Fatalf("invalid generated JSON:\n%s", artifact.content)
		}
	case strings.HasSuffix(path, ".toml"):
		var value map[string]any
		if err := toml.Unmarshal([]byte(artifact.content), &value); err != nil {
			t.Fatalf("invalid generated TOML: %v\n%s", err, artifact.content)
		}
	case strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml"):
		var value map[string]any
		if err := yaml.Unmarshal([]byte(artifact.content), &value); err != nil {
			t.Fatalf("invalid generated YAML: %v\n%s", err, artifact.content)
		}
	case strings.HasSuffix(path, ".ts"):
		file := writeRuntimeArtifact(t, filepath.Base(artifact.path), artifact.content)
		moduleURL := (&url.URL{Scheme: "file", Path: file}).String()
		runGeneratedCommand(t, exec.Command("node", "--experimental-strip-types", "--eval", "import("+strconv.Quote(moduleURL)+")"), "")
	case strings.HasSuffix(path, ".js"):
		file := writeRuntimeArtifact(t, filepath.Base(artifact.path), artifact.content)
		runGeneratedCommand(t, exec.Command("node", "--check", file), "")
	case strings.HasSuffix(path, ".py"):
		file := writeRuntimeArtifact(t, filepath.Base(artifact.path), artifact.content)
		runGeneratedCommand(t, exec.Command("python3", "-m", "py_compile", file), "")
	case strings.HasSuffix(path, ".sh"):
		file := writeRuntimeArtifact(t, filepath.Base(artifact.path), artifact.content)
		runGeneratedCommand(t, exec.Command("sh", "-n", file), "")
	default:
		if !strings.Contains(artifact.content, harnesspkg.ManagedMarker) {
			t.Fatalf("unrecognized generated artifact %q lacks managed marker", artifact.path)
		}
	}
}

type captureExecutable struct {
	command string
	path    string
}

func captureBinary(t *testing.T) captureExecutable {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "arguments")
	command := filepath.Join(dir, "aht-capture")
	script := `#!/bin/sh
{
  printf '%s\n' '---'
  printf '%s\n' "$@"
} >> "${AHT_CAPTURE:?}"
`
	writeTestFile(t, command, script, 0o700)
	return captureExecutable{command: command, path: capture}
}

func generatedCommandHook(t *testing.T, harness registry.Harness) string {
	t.Helper()
	adapter, ok := harnesscatalog.Find(harness)
	if !ok {
		t.Fatalf("find harness %s", harness)
	}
	installer, ok := adapter.(harnesspkg.Installable)
	if !ok {
		t.Fatalf("harness %s is not installable", harness)
	}
	for _, action := range installer.InstallPlan(captureBinaryCommand(t)).Actions {
		if plan, ok := action.(harnesspkg.JSONCommandHooksAction); ok && len(plan.Plan.Hooks) > 0 {
			return plan.Plan.Hooks[0].Command
		}
	}
	t.Fatalf("harness %s has no command hook", harness)
	return ""
}

func captureBinaryCommand(t *testing.T) string {
	t.Helper()
	return captureBinary(t).command
}

func generatedArtifactContent(t *testing.T, harness registry.Harness, suffix string) string {
	t.Helper()
	for _, artifact := range collectGeneratedArtifacts(t, captureExecutable{command: captureBinaryCommand(t), path: os.Getenv("AHT_CAPTURE")}) {
		if artifact.harness == harness && strings.HasSuffix(filepath.ToSlash(artifact.path), suffix) {
			return artifact.content
		}
	}
	t.Fatalf("generated artifact %s/%s not found", harness, suffix)
	return ""
}

func runNodeRuntime(t *testing.T, moduleName string, module string, driver string, extraFiles map[string]string) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "package.json"), `{"type":"module"}`, 0o600)
	writeTestFile(t, filepath.Join(dir, moduleName), module, 0o600)
	for path, content := range extraFiles {
		writeTestFile(t, filepath.Join(dir, path), content, 0o600)
	}
	driverPath := filepath.Join(dir, "driver.mjs")
	writeTestFile(t, driverPath, driver, 0o600)
	args := []string{driverPath}
	if strings.HasSuffix(moduleName, ".ts") {
		args = []string{"--experimental-strip-types", driverPath}
	}
	runGeneratedCommand(t, exec.Command("node", args...), "")
}

func writeRuntimeArtifact(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeTestFile(t, path, content, 0o600)
	return path
}

func writeTestFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create generated runtime directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write generated runtime file: %v", err)
	}
}

func runGeneratedCommand(t *testing.T, command *exec.Cmd, stdin string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command = exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	command.Env = os.Environ()
	command.Dir = filepath.Dir(command.Path)
	command.Stdin = strings.NewReader(stdin)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s: %v\n%s", strings.Join(command.Args, " "), err, output)
	}
}

func requireCapturedArguments(t *testing.T, path string, expected ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			content := string(data)
			matched := true
			for _, value := range expected {
				if !strings.Contains(content, value+"\n") {
					matched = false
					break
				}
			}
			if matched {
				return
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("read generated runtime capture: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("generated runtime arguments missing %q:\n%s", expected, data)
}

func requireRuntimeTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("Phase 3 generated-artifact validation requires %s: %v", name, err)
	}
	return path
}
