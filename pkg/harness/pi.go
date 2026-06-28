package harness

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	piExtensionName       = "agent-sessions-state.ts"
	piIntegrationID       = "pi"
	piIntegrationVersion  = 1
	piIntegrationSourceID = "pi-extension"
	piSessionFlag         = "--session"
)

//go:embed assets/pi/agent-sessions-state.ts.tmpl
var piExtensionTemplate string

type piHarness struct {
	baseAdapter
}

func piAdapter() Adapter {
	return piHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessPi, EnvKeys{
			SessionID:   []string{"PI_SESSION_ID"},
			SessionPath: []string{"PI_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"PI_PID"},
			Event:       nil,
		}),
	}
}

func (piHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{RenderedFileAction{Plan: RenderedFileInstallPlan{
			Path:        filepath.Join(piAgentDir(), "extensions", piExtensionName),
			Label:       "pi extension",
			ConfigLabel: "pi extension",
			Content: renderScriptTemplate(
				piExtensionTemplate,
				piIntegrationID,
				piIntegrationVersion,
				binary,
				piIntegrationSourceID,
			),
			JSONContent: nil,
		}}},
	}
}

func (piHarness) ResumeCommand(sessionID string, sessionPath string) []string {
	if sessionPath != "" {
		return []string{"pi", piSessionFlag, sessionPath}
	}
	if sessionID != "" {
		return []string{"pi", piSessionFlag, sessionID}
	}

	return nil
}

func piAgentDir() string {
	if value := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); value != "" {
		return value
	}

	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".pi", "agent")
	}

	return filepath.Join(".pi", "agent")
}
