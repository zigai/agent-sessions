package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func piAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessPi,
		Aliases:      nil,
		ProcessNames: []string{"pi"},
		Env: EnvKeys{
			SessionID:   []string{"PI_SESSION_ID"},
			SessionPath: []string{"PI_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"PI_PID"},
			Event:       nil,
		},
		Installable: true,
		ResumeCommand: func(sessionID string, sessionPath string) []string {
			if sessionPath != "" {
				return []string{"pi", sessionFlag, sessionPath}
			}
			if sessionID != "" {
				return []string{"pi", sessionFlag, sessionID}
			}

			return nil
		},
		PayloadValidator: nil,
		PayloadDefaults:  nil,
		Hook:             nil,
	}
}
