package harness

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const agyCommand = "agy"

func agyAdapter() Adapter {
	return Adapter{
		ID: registry.HarnessAgy,
		Aliases: []string{
			"antigravity",
			"antigravity-cli",
			"antigravity_cli",
			"google-antigravity",
			"google_antigravity",
		},
		ProcessNames: []string{agyCommand},
		Env: EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{agyCommand, "--conversation", sessionID}
		},
		PayloadValidator: payloadValidator[agyHookPayload](),
		PayloadDefaults:  agyPayloadDefaults,
	}
}

func agyPayloadDefaults(payload map[string]any) PayloadDefaults {
	workspacePath := firstArrayString(payload, "workspacePaths", "workspace_paths")
	toolCWD := nestedString(payload, "toolCall", "args", "Cwd")
	cwd := firstNonEmpty(toolCWD, workspacePath)

	attributes := make(map[string]string)
	addAttributeString(attributes, "agy_hook_event", payloadStringAny(payload, "hookEventName", "hook_event_name", "event"))
	addAttributeString(attributes, "agy_tool_name", nestedString(payload, "toolCall", "name"))
	addAttributeString(attributes, "agy_termination_reason", payloadStringAny(payload, "terminationReason", "termination_reason"))
	addAttributeString(attributes, "agy_error", payloadString(payload, "error"))
	addAttributeString(attributes, "agy_fully_idle", payloadBoolString(payload, "fullyIdle", "fully_idle"))

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "conversationId", "conversation_id"),
		SessionPath: payloadStringAny(payload, "transcriptPath", "transcript_path"),
		CWD:         cwd,
		ProjectRoot: workspacePath,
		Event:       payloadStringAny(payload, "hookEventName", "hook_event_name", "event"),
		Attributes:  attributes,
	}
}

func nestedString(payload map[string]any, path ...string) string {
	if len(path) == 0 {
		return ""
	}

	var current any = payload
	for _, part := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = currentMap[part]
	}

	text, ok := current.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

func firstArrayString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			text, textOK := item.(string)
			if textOK && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}

	return ""
}

func payloadBoolString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return strconv.FormatBool(typed)
		case string:
			return strings.TrimSpace(typed)
		default:
			if typed != nil {
				return fmt.Sprint(typed)
			}
		}
	}

	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
