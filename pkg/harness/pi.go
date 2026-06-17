package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func piAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessPi,
		ProcessNames: []string{"pi"},
		Env: EnvKeys{
			SessionID:   []string{"PI_SESSION_ID"},
			SessionPath: []string{"PI_SESSION_PATH"},
			PID:         []string{"PI_PID"},
		},
		Installable: true,
		ResumeCommand: func(sessionID string, sessionPath string) []string {
			if sessionPath != "" {
				return []string{"pi", "--session", sessionPath}
			}
			if sessionID != "" {
				return []string{"pi", "--session", sessionID}
			}

			return nil
		},
	}
}
