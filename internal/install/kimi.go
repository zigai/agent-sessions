package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	kimiCodeIntegrationSource = "kimi-code-hook"
	kimiCodeManagedStart      = "# BEGIN agent-sessions managed integration: kimi-code"
	kimiCodeManagedEnd        = "# END agent-sessions managed integration: kimi-code"
)

func installKimiCode(options Options) (Result, error) {
	path := filepath.Join(kimiCodeHome(), "config.toml")

	current, err := readKimiCodeConfig(path)
	if err != nil {
		return Result{}, err
	}

	next := upsertKimiCodeManagedBlock(current, kimiCodeHookBlock(options.Binary))
	changed := current != next

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating kimi-code config directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, []byte(next), 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing kimi-code hooks: %w", writeErr)
		}
	}

	message := "kimi-code hooks already installed"
	if changed {
		message = "kimi-code hooks installed"
	}
	if options.DryRun {
		message = "dry run: kimi-code hooks not written"
	}

	return Result{
		Harness: string(registry.HarnessKimiCode),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: next,
		Error:   "",
	}, nil
}

type kimiCodeHookSpec struct {
	event   string
	matcher string
	command string
	timeout int
}

func kimiCodeHookBlock(binary string) string {
	specs := []kimiCodeHookSpec{
		{
			event:   hookEventSessionStart,
			matcher: "startup|resume",
			command: kimiCodeSelfRefreshCommand(binary),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   hookEventSessionStart,
			matcher: "startup|resume",
			command: kimiCodeHookCommand(binary, registry.StateIdle, hookEventSessionStart),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "UserPromptSubmit",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateRunning, "UserPromptSubmit"),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "PermissionRequest",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateWaiting, "PermissionRequest"),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "PermissionResult",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateRunning, "PermissionResult"),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   hookEventStop,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateIdle, hookEventStop),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "StopFailure",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateIdle, "StopFailure"),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "Interrupt",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateIdle, "Interrupt"),
			timeout: hookTimeoutSeconds,
		},
		{
			event:   "SessionEnd",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.StateExited, "SessionEnd"),
			timeout: hookTimeoutSeconds,
		},
	}

	var builder strings.Builder
	builder.WriteString(kimiCodeManagedStart)
	builder.WriteByte('\n')
	builder.WriteString("# ")
	builder.WriteString(managedMarker)
	builder.WriteByte('\n')
	for _, spec := range specs {
		builder.WriteByte('\n')
		builder.WriteString("[[hooks]]\n")
		builder.WriteString("event = ")
		builder.WriteString(strconv.Quote(spec.event))
		builder.WriteByte('\n')
		if spec.matcher != "" {
			builder.WriteString("matcher = ")
			builder.WriteString(strconv.Quote(spec.matcher))
			builder.WriteByte('\n')
		}
		builder.WriteString("command = ")
		builder.WriteString(strconv.Quote(spec.command))
		builder.WriteByte('\n')
		builder.WriteString("timeout = ")
		builder.WriteString(strconv.Itoa(spec.timeout))
		builder.WriteByte('\n')
	}
	builder.WriteByte('\n')
	builder.WriteString(kimiCodeManagedEnd)
	builder.WriteByte('\n')

	return builder.String()
}

func kimiCodeHookCommand(binary string, state registry.State, event string) string {
	return reportHookCommand(binary, registry.HarnessKimiCode, state, event, kimiCodeIntegrationSource)
}

func kimiCodeSelfRefreshCommand(binary string) string {
	return strings.Join([]string{
		shellQuote(binary),
		"install-hooks", "kimi-code",
		"--binary", shellQuote(binary),
		"</dev/null", ">/dev/null", "2>&1", "&",
	}, " ")
}

func readKimiCodeConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	return string(data), nil
}

func upsertKimiCodeManagedBlock(current string, block string) string {
	cleaned := removeKimiCodeManagedBlock(current)

	return appendKimiCodeManagedBlock(cleaned, block)
}

func removeKimiCodeManagedBlock(current string) string {
	start := strings.Index(current, kimiCodeManagedStart)
	if start < 0 {
		return current
	}

	endOffset := strings.Index(current[start:], kimiCodeManagedEnd)
	if endOffset < 0 {
		return current
	}

	end := start + endOffset + len(kimiCodeManagedEnd)
	for end < len(current) && (current[end] == '\r' || current[end] == '\n') {
		end++
	}

	before := strings.TrimRight(current[:start], " \t\r\n")
	after := strings.TrimLeft(current[end:], "\r\n")
	switch {
	case before == "":
		return after
	case after == "":
		return before
	default:
		return before + "\n\n" + after
	}
}

func appendKimiCodeManagedBlock(current string, block string) string {
	trimmed := strings.TrimRight(current, " \t\r\n")
	if trimmed == "" {
		return block
	}

	return trimmed + "\n\n" + block
}

func kimiCodeHome() string {
	if value := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".kimi-code")
	}

	return ".kimi-code"
}
