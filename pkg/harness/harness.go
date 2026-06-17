package harness

import "github.com/zigai/agent-sessions/pkg/registry"

func ResumeCommandFor(harness registry.Harness, sessionID string, sessionPath string) []string {
	switch harness {
	case registry.HarnessCodex:
		if sessionID != "" {
			return []string{"codex", "resume", sessionID}
		}
	case registry.HarnessPi:
		if sessionPath != "" {
			return []string{"pi", "--session", sessionPath}
		}

		if sessionID != "" {
			return []string{"pi", "--session", sessionID}
		}
	case registry.HarnessOpenCode:
		if sessionID != "" {
			return []string{"opencode", "--session", sessionID}
		}
	}

	return nil
}

func WithResumeCommand(report registry.Report) registry.Report {
	if len(report.ResumeCommand) == 0 {
		report.ResumeCommand = ResumeCommandFor(report.Harness, report.SessionID, report.SessionPath)
	}

	return report
}
