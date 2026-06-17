package install

import (
	"maps"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	hookEventSessionStart = "SessionStart"
	hookEventStop         = "Stop"
)

func reportHookCommand(binary string, harness registry.Harness, state registry.State, event string, source string) string {
	return strings.Join([]string{
		shellQuote(binary),
		"report",
		"--harness", shellQuote(string(harness)),
		"--state", shellQuote(string(state)),
		"--event", shellQuote(event),
		"--source", shellQuote(source),
		"--attribute", shellQuote("agent_sessions_integration=" + source),
		"--raw-stdin",
		"--quiet",
	}, " ")
}

func commandHookGroup(command string, matcher string, statusMessage string) map[string]any {
	group := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       command,
				"timeout":       float64(hookTimeoutSeconds),
				"statusMessage": statusMessage,
			},
		},
	}
	if matcher != "" {
		group["matcher"] = matcher
	}

	return group
}

func upsertManagedCommandHookGroup(
	config map[string]any,
	event string,
	matcher string,
	command string,
	statusMessage string,
	isManaged func(string) bool,
) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		config["hooks"] = hooks
	}

	groups, ok := hooks[event].([]any)
	if !ok {
		groups = nil
	}

	managedCount, exactCount := countManagedCommandHookGroups(groups, command, isManaged)
	if managedCount == 1 && exactCount == 1 {
		return false
	}

	groups, _ = removeManagedCommandHookGroups(groups, isManaged)
	hooks[event] = append(groups, commandHookGroup(command, matcher, statusMessage))

	return true
}

func countManagedCommandHookGroups(groups []any, command string, isManaged func(string) bool) (int, int) {
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
			if !commandOK || !isManaged(hookCommand) {
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

func removeManagedCommandHookGroups(groups []any, isManaged func(string) bool) ([]any, bool) {
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
			if commandOK && isManaged(hookCommand) {
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
