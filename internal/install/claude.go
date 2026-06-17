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

const claudeIntegrationSource = "claude-hook"

func installClaude(options Options) (Result, error) {
	path := filepath.Join(claudeConfigDir(), "settings.json")

	config, err := readJSONObject(path)
	if err != nil {
		return Result{}, err
	}

	changed := false
	for _, hook := range claudeHooks(options.Binary) {
		updated := upsertClaudeHook(config, hook.event, hook.matcher, hook.command)
		changed = changed || updated
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encoding claude hooks: %w", err)
	}
	data = append(data, '\n')

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating claude config directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, data, 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing claude hooks: %w", writeErr)
		}
	}

	message := "claude hooks already installed"
	if changed {
		message = "claude hooks installed"
	}
	if options.DryRun {
		message = "dry run: claude hooks not written"
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

type claudeHook struct {
	event   string
	matcher string
	command string
}

func claudeHooks(binary string) []claudeHook {
	return []claudeHook{
		{
			event:   "SessionStart",
			matcher: "startup|resume|clear|compact",
			command: claudeSessionStartCommand(binary, registry.StateIdle, "SessionStart"),
		},
		{
			event:   "UserPromptSubmit",
			matcher: "",
			command: claudeHookCommand(binary, registry.StateRunning, "UserPromptSubmit"),
		},
		{
			event:   "Notification",
			matcher: "permission_prompt",
			command: claudeHookCommand(binary, registry.StateWaiting, "Notification"),
		},
		{
			event:   "Stop",
			matcher: "",
			command: claudeHookCommand(binary, registry.StateIdle, "Stop"),
		},
		{
			event:   "SessionEnd",
			matcher: "",
			command: claudeHookCommand(binary, registry.StateExited, "SessionEnd"),
		},
	}
}

func claudeHookCommand(binary string, state registry.State, event string) string {
	return reportHookCommand(binary, registry.HarnessClaude, state, event, claudeIntegrationSource)
}

func claudeSessionStartCommand(binary string, state registry.State, event string) string {
	selfRefresh := strings.Join([]string{
		shellQuote(binary),
		"install-hooks", "claude",
		"--binary", shellQuote(binary),
		"</dev/null", ">/dev/null", "2>&1", "&",
	}, " ")

	return selfRefresh + " " + claudeHookCommand(binary, state, event)
}

func upsertClaudeHook(config map[string]any, event string, matcher string, command string) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		config["hooks"] = hooks
	}

	groups, ok := hooks[event].([]any)
	if !ok {
		groups = nil
	}

	managedCount, exactCount := countManagedClaudeHooks(groups, command)
	if managedCount == 1 && exactCount == 1 {
		return false
	}

	groups, _ = removeManagedClaudeHooks(groups)
	hooks[event] = append(groups, commandHookGroup(command, matcher, managedMarker))

	return true
}

func countManagedClaudeHooks(groups []any, command string) (int, int) {
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
			if !commandOK || !isManagedClaudeHookCommand(hookCommand) {
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

func removeManagedClaudeHooks(groups []any) ([]any, bool) {
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
			if commandOK && isManagedClaudeHookCommand(hookCommand) {
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

func isManagedClaudeHookCommand(command string) bool {
	return strings.Contains(command, "agent_sessions_integration=claude-hook") ||
		strings.Contains(command, "--source claude-hook")
}

func claudeConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".claude")
	}

	return ".claude"
}
