package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const claudeIntegrationSource = "claude-hook"

func installClaude(options Options) (Result, error) {
	return installJSONHookFile(options, jsonHookFileInstall{
		Harness:                 registry.HarnessClaude,
		Path:                    filepath.Join(claudeConfigDir(), "settings.json"),
		Apply:                   applyClaudeHooks(options.Binary),
		EncodeError:             "encoding claude hooks",
		CreateDirError:          "creating claude config directory",
		WriteError:              "writing claude hooks",
		InstalledMessage:        "claude hooks installed",
		AlreadyInstalledMessage: "claude hooks already installed",
		DryRunMessage:           "dry run: claude hooks not written",
	})
}

type claudeHook struct {
	event   string
	matcher string
	command string
}

func claudeHooks(binary string) []claudeHook {
	return []claudeHook{
		{
			event:   hookEventSessionStart,
			matcher: "startup|resume|clear|compact",
			command: claudeSessionStartCommand(binary, registry.StateIdle, hookEventSessionStart),
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
			event:   hookEventStop,
			matcher: "",
			command: claudeHookCommand(binary, registry.StateIdle, hookEventStop),
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

func applyClaudeHooks(binary string) func(map[string]any) bool {
	return func(config map[string]any) bool {
		changed := false
		for _, hook := range claudeHooks(binary) {
			updated := upsertManagedCommandHookGroup(
				config,
				hook.event,
				hook.matcher,
				hook.command,
				managedMarker,
				isManagedClaudeHookCommand,
			)
			changed = changed || updated
		}

		return changed
	}
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
