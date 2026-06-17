package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const hookTimeoutSeconds = 5

func installCodex(options Options) (Result, error) {
	return installJSONHookFile(options, jsonHookFileInstall{
		Harness:                 registry.HarnessCodex,
		Path:                    filepath.Join(codexHome(), "hooks.json"),
		Apply:                   applyCodexHooks(options.Binary),
		EncodeError:             "encoding codex hooks",
		CreateDirError:          "creating codex config directory",
		WriteError:              "writing codex hooks",
		InstalledMessage:        "codex hooks installed",
		AlreadyInstalledMessage: "codex hooks already installed",
		DryRunMessage:           "dry run: codex hooks not written",
	})
}

type codexHook struct {
	event   string
	matcher string
	command string
}

func codexHooks(binary string) []codexHook {
	return []codexHook{
		{
			event:   hookEventSessionStart,
			matcher: "startup|resume|clear|compact",
			command: codexHookCommand(binary, registry.StateIdle, hookEventSessionStart),
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
			event:   hookEventStop,
			matcher: "",
			command: codexHookCommand(binary, registry.StateIdle, hookEventStop),
		},
	}
}

func codexHookCommand(binary string, state registry.State, event string) string {
	return reportHookCommand(binary, registry.HarnessCodex, state, event, "codex-hook")
}

func applyCodexHooks(binary string) func(map[string]any) bool {
	return func(config map[string]any) bool {
		changed := false
		for _, hook := range codexHooks(binary) {
			updated := upsertManagedCommandHookGroup(
				config,
				hook.event,
				hook.matcher,
				hook.command,
				"Recording agent session",
				isManagedCodexHookCommand,
			)
			changed = changed || updated
		}

		return changed
	}
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
