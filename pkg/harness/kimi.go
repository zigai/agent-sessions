package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
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
			event:   "SessionEnd",
			matcher: "",
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

func kimiCodeHookCommand(binary string, transition any, event string) string {
	return ReportHookCommand(binary, registry.HarnessKimiCode, transition, event, kimiCodeIntegrationSource)
}

func kimiCodePayloadDefaults(payload map[string]any) PayloadDefaults {
	sessionID := payloadString(payload, "session_id")
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
		SessionPath: kimiCodeSessionPath(sessionID),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
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

func kimiCodeSessionPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	file, err := os.Open(filepath.Join(kimiCodeHome(), "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, kimiCodeSessionIndexInitialBufferSize), kimiCodeSessionIndexMaxBufferSize)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &entry); err != nil {
			continue
		}
		sessionDir := payloadString(entry, "sessionDir")
		if payloadString(entry, "sessionId") == sessionID && sessionDir != "" {
			return sessionDir
		}
	}

	return ""
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
