package harness

import "github.com/zigai/agent-sessions/pkg/registry"

const kiloCommand = "kilo"

func kiloAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessKilo,
		Aliases:      []string{"kilocode", "kilo-code", "kilo_code"},
		ProcessNames: []string{kiloCommand, "kilocode", "kilo-code", "kilo_code"},
		Env: EnvKeys{
			SessionID:   []string{"KILO_SESSION_ID", "KILOCODE_SESSION_ID"},
			SessionPath: []string{"KILO_SESSION_PATH", "KILOCODE_SESSION_PATH"},
			ProjectRoot: []string{"KILO_PROJECT_ROOT", "KILOCODE_PROJECT_ROOT"},
			PID:         []string{"KILO_PID", "KILOCODE_PID"},
			Event:       []string{"KILO_EVENT", "KILOCODE_EVENT"},
		},
		Installable: true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{kiloCommand, sessionFlag, sessionID}
		},
		PayloadDefaults: nil,
	}
}
