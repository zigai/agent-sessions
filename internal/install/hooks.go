package install

import (
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
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
