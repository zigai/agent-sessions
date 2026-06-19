package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func grokAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessGrok,
		Aliases:      []string{"grok-build", "grok_build"},
		ProcessNames: []string{"grok", "grok-build"},
		Env: EnvKeys{
			SessionID:   []string{"GROK_SESSION_ID"},
			SessionPath: nil,
			ProjectRoot: []string{"GROK_WORKSPACE_ROOT"},
			PID:         nil,
			Event:       []string{"GROK_HOOK_EVENT"},
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{"grok", "--resume", sessionID}
		},
		PayloadValidator: grokPayloadValidator,
		PayloadDefaults:  grokPayloadDefaults,
		Hook:             nil,
	}
}

func grokPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "grok_hook_event", payloadStringAny(payload, "hookEventName", "hook_event_name"))
	addAttributeString(attributes, "grok_tool_name", payloadStringAny(payload, "toolName", "tool_name"))
	addAttributeString(attributes, "grok_notification_type", payloadStringAny(
		payload,
		"notificationType",
		"notification_type",
		"type",
	))

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "sessionId", "session_id"),
		SessionPath: "",
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: payloadStringAny(payload, "workspaceRoot", "workspace_root"),
		Event:       payloadStringAny(payload, "hookEventName", "hook_event_name"),
		Attributes:  attributes,
	}
}
