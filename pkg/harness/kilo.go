package harness

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	kiloCommand     = "kilo"
	kiloSessionFlag = "--session"
)

const (
	kiloPluginName        = "agent-sessions-state.ts"
	kiloIntegrationID     = "kilo"
	kiloIntegrationSource = "kilo-plugin"
)

//go:embed assets/kilo/agent-sessions-state.ts.tmpl
var kiloPluginTemplate string

type kiloHarness struct {
	baseAdapter
}

func kiloAdapter() Adapter {
	return kiloHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessKilo, EnvKeys{
			SessionID:   []string{"KILO_SESSION_ID", "KILOCODE_SESSION_ID"},
			SessionPath: []string{"KILO_SESSION_PATH", "KILOCODE_SESSION_PATH"},
			ProjectRoot: []string{"KILO_PROJECT_ROOT", "KILOCODE_PROJECT_ROOT"},
			PID:         []string{"KILO_PID", "KILOCODE_PID"},
			Event:       []string{"KILO_EVENT", "KILOCODE_EVENT"},
		}),
	}
}

func (kiloHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{RenderedFileAction{Plan: RenderedFileInstallPlan{
			Path:        filepath.Join(kiloConfigDir(), "plugin", kiloPluginName),
			Label:       "kilo plugin",
			ConfigLabel: "kilo plugin",
			Content: renderScriptTemplate(
				kiloPluginTemplate,
				kiloIntegrationID,
				binary,
				kiloIntegrationSource,
			),
			JSONContent: nil,
		}}},
	}
}

func (kiloHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{kiloCommand, kiloSessionFlag, sessionID}
}

func kiloConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		return filepath.Join(value, "kilo")
	}

	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "kilo")
	}

	return filepath.Join(".config", "kilo")
}
