package harness

import "github.com/zigai/agent-sessions/pkg/registry"

const codexCommand = "codex"

func codexAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessCodex,
		Aliases:      nil,
		ProcessNames: []string{codexCommand},
		Env: EnvKeys{
			SessionID:   []string{"CODEX_SESSION_ID"},
			SessionPath: []string{"CODEX_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"CODEX_PID"},
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{codexCommand, "resume", sessionID}
		},
		PayloadDefaults: codexPayloadDefaults,
	}
}

func codexPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "codex_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "codex_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "codex_permission_mode", payloadString(payload, "permission_mode"))
	addAttributeString(attributes, "codex_model", payloadString(payload, "model"))
	addAttributeString(attributes, "codex_turn_id", payloadString(payload, "turn_id"))

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}
