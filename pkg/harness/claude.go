package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func claudeAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessClaude,
		Aliases:      []string{"claude-code", "claude_code"},
		ProcessNames: []string{"claude"},
		Env: EnvKeys{
			SessionID:   []string{"CLAUDE_SESSION_ID"},
			SessionPath: []string{"CLAUDE_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"CLAUDE_PID"},
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{"claude", "--resume", sessionID}
		},
		PayloadValidator: payloadValidator[claudeHookPayload](),
		PayloadDefaults:  claudePayloadDefaults,
		Hook:             nil,
	}
}

func claudePayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "claude_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "claude_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "claude_permission_mode", payloadString(payload, "permission_mode"))
	addAttributeString(attributes, "claude_model", payloadString(payload, "model"))
	addAttributeString(attributes, "claude_notification_type", payloadStringAny(payload, "notification_type", "type"))
	addAttributeString(attributes, "claude_session_end_reason", payloadString(payload, "reason"))

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}
