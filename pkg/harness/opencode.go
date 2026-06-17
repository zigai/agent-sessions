package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func openCodeAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessOpenCode,
		Aliases:      []string{"open-code", "open_code"},
		ProcessNames: []string{"opencode"},
		Env: EnvKeys{
			SessionID:   []string{"OPENCODE_SESSION_ID"},
			SessionPath: []string{"OPENCODE_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"OPENCODE_PID"},
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{"opencode", "--session", sessionID}
		},
		PayloadDefaults: nil,
	}
}
