package harness

import (
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const cursorCommand = "cursor-agent"

func cursorAdapter() Adapter {
	return Adapter{
		ID:      registry.HarnessCursor,
		Aliases: []string{"cursor-agent", "cursor_agent", "cursor-cli", "cursor_cli"},
		ProcessNames: []string{
			cursorCommand,
			"agent",
		},
		Env: EnvKeys{
			SessionID:   nil,
			SessionPath: []string{"CURSOR_TRANSCRIPT_PATH"},
			ProjectRoot: []string{"CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"},
			PID:         nil,
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{cursorCommand, "--resume", sessionID}
		},
		PayloadValidator: payloadValidator[cursorHookPayload](),
		PayloadDefaults:  cursorPayloadDefaults,
	}
}

func cursorPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "cursor_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "cursor_model", payloadString(payload, "model"))
	addAttributeString(attributes, "cursor_version", payloadString(payload, "cursor_version"))
	addAttributeString(attributes, "cursor_composer_mode", payloadString(payload, "composer_mode"))
	addAttributeString(attributes, "cursor_session_end_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "cursor_final_status", payloadString(payload, "final_status"))
	addAttributeString(attributes, "cursor_stop_status", payloadString(payload, "status"))
	addAttributeBool(attributes, "cursor_is_background_agent", payload, "is_background_agent")
	addAttributeBool(attributes, "cursor_sandbox", payload, "sandbox")

	projectRoot := firstPayloadString(payload, "workspace_roots")
	cwd := payloadString(payload, "cwd")
	if cwd == "" {
		cwd = projectRoot
	}

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "session_id", "conversation_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         cwd,
		ProjectRoot: projectRoot,
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func firstPayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return ""
	}

	text, ok := items[0].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

func addAttributeBool(attributes map[string]string, attributeKey string, payload map[string]any, payloadKey string) {
	value, ok := payload[payloadKey].(bool)
	if !ok {
		return
	}

	attributes[attributeKey] = strconv.FormatBool(value)
}
