package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	grokHookFileName      = "agent-sessions-state.json"
	grokIntegrationSource = "grok-hook"
)

func installGrok(options Options) (Result, error) {
	path := filepath.Join(grokHome(), "hooks", grokHookFileName)

	config := grokHookConfig(options.Binary)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encoding grok hooks: %w", err)
	}
	data = append(data, '\n')
	script := string(data)

	changed, err := fileNeedsUpdate(path, script, options.Force)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating grok hooks directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, data, 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing grok hooks: %w", writeErr)
		}
	}

	message := "grok hooks already installed"
	if changed {
		message = "grok hooks installed"
	}
	if options.DryRun {
		message = "dry run: grok hooks not written"
	}

	return Result{
		Harness: string(registry.HarnessGrok),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: script,
		Error:   "",
	}, nil
}

type grokHookSpec struct {
	event   string
	matcher string
	command string
}

func grokHookConfig(binary string) map[string]any {
	specs := []grokHookSpec{
		{
			event:   "SessionStart",
			matcher: "",
			command: grokSelfRefreshCommand(binary),
		},
		{
			event:   "SessionStart",
			matcher: "",
			command: grokHookCommand(binary, registry.StateIdle, "SessionStart"),
		},
		{
			event:   "UserPromptSubmit",
			matcher: "",
			command: grokHookCommand(binary, registry.StateRunning, "UserPromptSubmit"),
		},
		{
			event:   "Notification",
			matcher: "approval_required",
			command: grokHookCommand(binary, registry.StateWaiting, "Notification"),
		},
		{
			event:   "Stop",
			matcher: "",
			command: grokHookCommand(binary, registry.StateIdle, "Stop"),
		},
		{
			event:   "SessionEnd",
			matcher: "",
			command: grokHookCommand(binary, registry.StateExited, "SessionEnd"),
		},
	}

	hooks := make(map[string]any)
	for _, spec := range specs {
		existing, ok := hooks[spec.event].([]any)
		if !ok {
			existing = nil
		}
		hooks[spec.event] = append(existing, commandHookGroup(spec.command, spec.matcher, managedMarker))
	}

	return map[string]any{"hooks": hooks}
}

func grokHookCommand(binary string, state registry.State, event string) string {
	return reportHookCommand(binary, registry.HarnessGrok, state, event, grokIntegrationSource)
}

func grokSelfRefreshCommand(binary string) string {
	return strings.Join([]string{
		shellQuote(binary),
		"install-hooks", "grok",
		"--binary", shellQuote(binary),
		">/dev/null", "2>&1", "&",
	}, " ")
}

func grokHome() string {
	if value := strings.TrimSpace(os.Getenv("GROK_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".grok")
	}

	return ".grok"
}
