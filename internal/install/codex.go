package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const hookTimeoutSeconds = 5

func installCodex(options Options) (Result, error) {
	path := filepath.Join(codexHome(), "hooks.json")

	config, err := readJSONObject(path)
	if err != nil {
		return Result{}, err
	}

	changed := false
	for _, hook := range codexHooks(options.Binary) {
		updated := upsertCodexHook(config, hook.event, hook.matcher, hook.command)
		changed = changed || updated
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encoding codex hooks: %w", err)
	}
	data = append(data, '\n')

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating codex config directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, data, 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing codex hooks: %w", writeErr)
		}
	}

	message := "codex hooks already installed"
	if changed {
		message = "codex hooks installed"
	}
	if options.DryRun {
		message = "dry run: codex hooks not written"
	}

	return Result{
		Harness: string(options.Harness),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: string(data),
		Error:   "",
	}, nil
}

type codexHook struct {
	event   string
	matcher string
	command string
}

func codexHooks(binary string) []codexHook {
	return []codexHook{
		{
			event:   "SessionStart",
			matcher: "startup|resume|clear|compact",
			command: codexHookCommand(binary, registry.StateIdle, "SessionStart"),
		},
		{
			event:   "UserPromptSubmit",
			matcher: "",
			command: codexHookCommand(binary, registry.StateRunning, "UserPromptSubmit"),
		},
		{
			event:   "PermissionRequest",
			matcher: "*",
			command: codexHookCommand(binary, registry.StateWaiting, "PermissionRequest"),
		},
		{
			event:   "Stop",
			matcher: "",
			command: codexHookCommand(binary, registry.StateIdle, "Stop"),
		},
	}
}

func codexHookCommand(binary string, state registry.State, event string) string {
	return reportHookCommand(binary, registry.HarnessCodex, state, event, "codex-hook")
}

func upsertCodexHook(config map[string]any, event string, matcher string, command string) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		config["hooks"] = hooks
	}

	groups, ok := hooks[event].([]any)
	if !ok {
		groups = nil
	}

	managedCount, exactCount := countManagedCodexHooks(groups, command)
	if managedCount == 1 && exactCount == 1 {
		return false
	}

	groups, _ = removeManagedCodexHooks(groups)
	hooks[event] = append(groups, commandHookGroup(command, matcher, "Recording agent session"))

	return true
}

func countManagedCodexHooks(groups []any, command string) (int, int) {
	managedCount := 0
	exactCount := 0
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			continue
		}
		hookValues, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, hookValue := range hookValues {
			hook, hookOK := hookValue.(map[string]any)
			if !hookOK {
				continue
			}
			hookCommand, commandOK := hook["command"].(string)
			if !commandOK || !isManagedCodexHookCommand(hookCommand) {
				continue
			}
			managedCount++
			if hookCommand == command {
				exactCount++
			}
		}
	}

	return managedCount, exactCount
}

func removeManagedCodexHooks(groups []any) ([]any, bool) {
	cleanedGroups := make([]any, 0, len(groups))
	removed := false
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			cleanedGroups = append(cleanedGroups, groupValue)
			continue
		}
		hookValues, ok := group["hooks"].([]any)
		if !ok {
			cleanedGroups = append(cleanedGroups, groupValue)
			continue
		}

		cleanedHooks := make([]any, 0, len(hookValues))
		for _, hookValue := range hookValues {
			hook, hookOK := hookValue.(map[string]any)
			if !hookOK {
				cleanedHooks = append(cleanedHooks, hookValue)
				continue
			}
			hookCommand, commandOK := hook["command"].(string)
			if commandOK && isManagedCodexHookCommand(hookCommand) {
				removed = true
				continue
			}
			cleanedHooks = append(cleanedHooks, hookValue)
		}
		if len(cleanedHooks) == 0 {
			removed = true
			continue
		}

		cleanedGroup := make(map[string]any, len(group))
		maps.Copy(cleanedGroup, group)
		cleanedGroup["hooks"] = cleanedHooks
		cleanedGroups = append(cleanedGroups, cleanedGroup)
	}

	return cleanedGroups, removed
}

func isManagedCodexHookCommand(command string) bool {
	return strings.Contains(command, "agent_sessions_integration=codex-hook") ||
		strings.Contains(command, "--source codex-hook")
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"hooks": map[string]any{}}, nil
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"hooks": map[string]any{}}, nil
	}

	var config map[string]any
	unmarshalErr := json.Unmarshal(data, &config)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, unmarshalErr)
	}
	if config == nil {
		config = map[string]any{"hooks": map[string]any{}}
	}

	return config, nil
}

func codexHome() string {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".codex")
	}

	return ".codex"
}
