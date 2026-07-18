package harness

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	kimiCommand     = "kimi"
	kimiSessionFlag = "--session"
)

const (
	kimiCodeIntegrationSource             = "kimi-code-hook"
	kimiCodeManagedIntegrationStart       = "# BEGIN agent-sessions managed integration: kimi-code"
	kimiCodeManagedIntegrationEnd         = "# END agent-sessions managed integration: kimi-code"
	kimiCodeSessionIndexInitialBufferSize = 64 * 1024
	kimiCodeSessionIndexMaxBufferSize     = 1024 * 1024
)

type kimiCodeHarness struct {
	baseAdapter
}

func kimiCodeAdapter() Adapter {
	return kimiCodeHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessKimiCode, emptyEnvKeys()),
	}
}

func (kimiCodeHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{ManagedTextBlockAction{Plan: ManagedTextBlockInstallPlan{
			Path:        filepath.Join(kimiCodeHome(), "config.toml"),
			Label:       "kimi-code hooks",
			ConfigLabel: "kimi-code config",
			StartMarker: kimiCodeManagedIntegrationStart,
			EndMarker:   kimiCodeManagedIntegrationEnd,
			Block:       kimiCodeHookBlock(binary),
		}}},
	}
}

func (kimiCodeHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{kimiCommand, kimiSessionFlag, sessionID}
}

func (kimiCodeHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return payloadValidator[kimiCodeHookPayload]()(rawPayload)
}

func (kimiCodeHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	defaults, _ := kimiCodePayloadDefaults(payload)

	return defaults
}

func (kimiCodeHarness) payloadDefaultsWithError(payload map[string]any) (PayloadDefaults, error) {
	return kimiCodePayloadDefaults(payload)
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
			event:   HookEventSessionStart,
			matcher: "startup|resume",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, HookEventSessionStart),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   HookEventUserPromptSubmit,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, HookEventUserPromptSubmit),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   HookEventPreToolUse,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, HookEventPreToolUse),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   HookEventPostToolUse,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, HookEventPostToolUse),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   HookEventPostToolUseFailure,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, HookEventPostToolUseFailure),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "PermissionRequest",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityWaiting, "PermissionRequest"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "PermissionResult",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, "PermissionResult"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   HookEventStop,
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, HookEventStop),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "StopFailure",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, "StopFailure"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "Interrupt",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, "Interrupt"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "SubagentStart",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, "SubagentStart"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "SubagentStop",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, "SubagentStop"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "PreCompact",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityRunning, "PreCompact"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "PostCompact",
			matcher: "",
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, "PostCompact"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "Notification",
			matcher: `task\.completed`,
			command: kimiCodeHookCommand(binary, registry.ActivityIdle, "Notification"),
			timeout: HookTimeoutSeconds,
		},
		{
			event:   "SessionEnd",
			matcher: "exit",
			command: kimiCodeHookCommand(binary, registry.PresenceGone, "SessionEnd"),
			timeout: HookTimeoutSeconds,
		},
	}

	var builder strings.Builder
	builder.WriteString(kimiCodeManagedIntegrationStart)
	builder.WriteByte('\n')
	builder.WriteString("# ")
	builder.WriteString(ManagedMarker)
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
	builder.WriteString(kimiCodeManagedIntegrationEnd)
	builder.WriteByte('\n')

	return builder.String()
}

func kimiCodeHookCommand[T hookTransition](binary string, transition T, event string) string {
	return ReportHookCommand(binary, registry.HarnessKimiCode, transition, event, kimiCodeIntegrationSource)
}

func kimiCodePayloadDefaults(payload map[string]any) (PayloadDefaults, error) {
	sessionID := payloadString(payload, "session_id")
	sessionPath, err := kimiCodeSessionPath(sessionID)
	if err != nil {
		return PayloadDefaults{}, err
	}
	attributes := make(map[string]string)
	addAttributeString(attributes, "kimi_code_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "kimi_code_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "kimi_code_tool_name", payloadString(payload, "tool_name"))
	addAttributeString(attributes, "kimi_code_turn_id", payloadScalarString(payload, "turn_id"))
	addAttributeString(attributes, "kimi_code_decision", payloadString(payload, "decision"))
	addAttributeString(attributes, "kimi_code_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "kimi_code_notification_type", payloadStringAny(payload, "notification_type", "type"))

	return PayloadDefaults{
		SessionID:   sessionID,
		SessionPath: sessionPath,
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}, nil
}

func payloadScalarString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func kimiCodeSessionPath(sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}

	indexPath := filepath.Join(kimiCodeHome(), "session_index.jsonl")
	file, err := os.Open(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("opening Kimi Code session index: %w", err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, kimiCodeSessionIndexInitialBufferSize), kimiCodeSessionIndexMaxBufferSize)
	sessionPath := ""
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &entry); err != nil {
			continue
		}
		sessionDir := payloadString(entry, "sessionDir")
		if payloadString(entry, "sessionId") == sessionID && sessionDir != "" {
			sessionPath = sessionDir
			break
		}
	}
	scanErr := scanner.Err()
	closeErr := file.Close()
	if scanErr != nil {
		return "", errors.Join(fmt.Errorf("scanning Kimi Code session index %s: %w", indexPath, scanErr), closeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("closing Kimi Code session index %s: %w", indexPath, closeErr)
	}

	return sessionPath, nil
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
