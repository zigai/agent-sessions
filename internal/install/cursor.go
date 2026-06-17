package install

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const cursorIntegrationSource = "cursor-hook"

func installCursor(options Options) (Result, error) {
	path := filepath.Join(cursorHome(), "hooks.json")

	config, err := readJSONObject(path)
	if err != nil {
		return Result{}, err
	}

	changed := ensureCursorVersion(config)
	for _, hook := range cursorHooks(options.Binary) {
		updated := upsertCursorHook(config, hook.event, hook.command)
		changed = changed || updated
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encoding cursor hooks: %w", err)
	}
	data = append(data, '\n')

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating cursor config directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, data, 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing cursor hooks: %w", writeErr)
		}
	}

	message := "cursor hooks already installed"
	if changed {
		message = "cursor hooks installed"
	}
	if options.DryRun {
		message = "dry run: cursor hooks not written"
	}

	return Result{
		Harness: string(registry.HarnessCursor),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: string(data),
		Error:   "",
	}, nil
}

type cursorHook struct {
	event   string
	command string
}

func cursorHooks(binary string) []cursorHook {
	return []cursorHook{
		{
			event:   "sessionStart",
			command: cursorHookCommand(binary, registry.StateIdle, "sessionStart", "{}"),
		},
		{
			event:   "beforeSubmitPrompt",
			command: cursorHookCommand(binary, registry.StateRunning, "beforeSubmitPrompt", `{"continue":true}`),
		},
		{
			event:   "beforeShellExecution",
			command: cursorHookCommand(binary, registry.StateWaiting, "beforeShellExecution", `{"permission":"allow"}`),
		},
		{
			event:   "afterShellExecution",
			command: cursorHookCommand(binary, registry.StateRunning, "afterShellExecution", "{}"),
		},
		{
			event:   "stop",
			command: cursorHookCommand(binary, registry.StateIdle, "stop", "{}"),
		},
		{
			event:   "sessionEnd",
			command: cursorHookCommand(binary, registry.StateExited, "sessionEnd", "{}"),
		},
	}
}

func cursorHookCommand(binary string, state registry.State, event string, hookOutput string) string {
	report := strings.Join([]string{
		shellQuote(binary),
		"report",
		"--harness", shellQuote(string(registry.HarnessCursor)),
		"--state", shellQuote(string(state)),
		"--event", shellQuote(event),
		"--source", shellQuote(cursorIntegrationSource),
		"--attribute", shellQuote("agent_sessions_integration=" + cursorIntegrationSource),
		"--raw-stdin-defaults-only",
		"--quiet",
	}, " ")

	return report + " >/dev/null 2>&1 || true; printf '%s\\n' " + shellQuote(hookOutput)
}

func ensureCursorVersion(config map[string]any) bool {
	if _, ok := config["version"]; ok {
		return false
	}

	config["version"] = float64(1)

	return true
}

func upsertCursorHook(config map[string]any, event string, command string) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		config["hooks"] = hooks
	}

	definitions, ok := hooks[event].([]any)
	if !ok {
		definitions = nil
	}

	managedCount, exactCount := countManagedCursorHooks(definitions, command)
	if managedCount == 1 && exactCount == 1 {
		return false
	}

	definitions, _ = removeManagedCursorHooks(definitions)
	hooks[event] = append(definitions, map[string]any{
		"command": command,
		"timeout": float64(hookTimeoutSeconds),
	})

	return true
}

func countManagedCursorHooks(definitions []any, command string) (int, int) {
	managedCount := 0
	exactCount := 0
	for _, definitionValue := range definitions {
		definition, ok := definitionValue.(map[string]any)
		if !ok {
			continue
		}
		hookCommand, commandOK := definition["command"].(string)
		if !commandOK || !isManagedCursorHookCommand(hookCommand) {
			continue
		}
		managedCount++
		if hookCommand == command {
			exactCount++
		}
	}

	return managedCount, exactCount
}

func removeManagedCursorHooks(definitions []any) ([]any, bool) {
	cleanedDefinitions := make([]any, 0, len(definitions))
	removed := false
	for _, definitionValue := range definitions {
		definition, ok := definitionValue.(map[string]any)
		if !ok {
			cleanedDefinitions = append(cleanedDefinitions, definitionValue)
			continue
		}
		hookCommand, commandOK := definition["command"].(string)
		if commandOK && isManagedCursorHookCommand(hookCommand) {
			removed = true
			continue
		}

		cleanedDefinition := make(map[string]any, len(definition))
		maps.Copy(cleanedDefinition, definition)
		cleanedDefinitions = append(cleanedDefinitions, cleanedDefinition)
	}

	return cleanedDefinitions, removed
}

func isManagedCursorHookCommand(command string) bool {
	return strings.Contains(command, "agent_sessions_integration=cursor-hook") ||
		strings.Contains(command, "--source cursor-hook")
}

func cursorHome() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cursor")
	}

	return ".cursor"
}
