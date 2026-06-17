package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	agyPluginName         = "agent-sessions-state"
	agyMarkerFileName     = ".agent-sessions-managed"
	agyIntegrationID      = "agy"
	agyIntegrationVersion = 1
	agyIntegrationSource  = "agy-hook"
)

func installAgy(options Options) (Result, error) {
	dir := filepath.Join(agyCLIHome(), "plugins", agyPluginName)
	files, err := agyPluginFiles(options.Binary)
	if err != nil {
		return Result{}, err
	}

	managed, err := agyPluginManaged(dir)
	if err != nil {
		return Result{}, err
	}
	if !managed && !options.Force {
		return Result{}, fmt.Errorf("%w: %s; pass --force to replace it", errForeignFile, dir)
	}

	changed, err := agyPluginNeedsUpdate(dir, files)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(dir, 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating agy plugin directory: %w", mkdirErr)
		}

		for name, content := range files {
			path := filepath.Join(dir, name)
			writeErr := os.WriteFile(path, []byte(content), 0o600)
			if writeErr != nil {
				return Result{}, fmt.Errorf("writing agy plugin file %s: %w", path, writeErr)
			}
		}
	}

	message := "agy plugin installed"
	if !changed {
		message = "agy plugin already installed"
	}
	if options.DryRun {
		message = "dry run: agy plugin not written"
	}

	return Result{
		Harness: string(registry.HarnessAgy),
		Path:    dir,
		Changed: changed,
		Message: message,
		Snippet: agyPluginSnippet(files),
		Error:   "",
	}, nil
}

func agyPluginFiles(binary string) (map[string]string, error) {
	manifest, err := json.MarshalIndent(map[string]any{
		"name": agyPluginName,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding agy plugin manifest: %w", err)
	}

	hooks, err := json.MarshalIndent(agyHookConfig(binary), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding agy hooks: %w", err)
	}

	return map[string]string{
		"plugin.json":     string(append(manifest, '\n')),
		"hooks.json":      string(append(hooks, '\n')),
		agyMarkerFileName: agyMarkerContent(),
	}, nil
}

func agyHookConfig(binary string) map[string]any {
	return map[string]any{
		agyPluginName: map[string]any{
			"PreInvocation":  []any{agyHookHandler(binary, "PreInvocation")},
			"PostInvocation": []any{agyHookHandler(binary, "PostInvocation")},
			"PreToolUse":     []any{agyToolHookGroup(binary, "PreToolUse")},
			"PostToolUse":    []any{agyToolHookGroup(binary, "PostToolUse")},
			"Stop":           []any{agyHookHandler(binary, "Stop")},
		},
	}
}

func agyToolHookGroup(binary string, event string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{
			agyHookHandler(binary, event),
		},
	}
}

func agyHookHandler(binary string, event string) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": agyHookCommand(binary, event),
		"timeout": float64(hookTimeoutSeconds),
	}
}

func agyHookCommand(binary string, event string) string {
	return strings.Join([]string{
		shellQuote(binary),
		"agy-hook",
		"--event", shellQuote(event),
	}, " ")
}

func agyPluginManaged(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking agy plugin directory: %w", err)
	}
	if !info.IsDir() {
		return false, nil
	}

	marker, err := os.ReadFile(filepath.Join(dir, agyMarkerFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("reading agy plugin marker: %w", err)
	}

	return strings.Contains(string(marker), managedMarker), nil
}

func agyPluginNeedsUpdate(dir string, files map[string]string) (bool, error) {
	for name, content := range files {
		path := filepath.Join(dir, name)
		current, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}

			return false, fmt.Errorf("reading agy plugin file %s: %w", path, err)
		}
		if string(current) != content {
			return true, nil
		}
	}

	return false, nil
}

func agyPluginSnippet(files map[string]string) string {
	names := []string{"plugin.json", "hooks.json", agyMarkerFileName}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, "== "+name+" ==\n"+files[name])
	}

	return strings.Join(parts, "\n")
}

func agyMarkerContent() string {
	return strings.Join([]string{
		managedMarker,
		"AGENT_SESSIONS_INTEGRATION_ID=" + agyIntegrationID,
		"AGENT_SESSIONS_INTEGRATION_VERSION=" + fmt.Sprint(agyIntegrationVersion),
		"AGENT_SESSIONS_SOURCE=" + agyIntegrationSource,
		"",
	}, "\n")
}

func agyCLIHome() string {
	if value := strings.TrimSpace(os.Getenv("AGY_CLI_HOME")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLI_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".gemini", "antigravity-cli")
	}

	return filepath.Join(".gemini", "antigravity-cli")
}
