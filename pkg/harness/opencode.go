package harness

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	openCodePluginName        = "agent-sessions-state.ts"
	openCodeIntegrationID     = "opencode"
	openCodeIntegrationSource = "opencode-plugin"
	openCodeSessionFlag       = "--session"
)

//go:embed assets/opencode/agent-sessions-state.ts.tmpl
var openCodePluginTemplate string

type openCodeHarness struct {
	baseAdapter
}

func openCodeAdapter() Adapter {
	return openCodeHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessOpenCode, EnvKeys{
			SessionID:   []string{"OPENCODE_SESSION_ID"},
			SessionPath: []string{"OPENCODE_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"OPENCODE_PID"},
			Event:       nil,
		}),
	}
}

func (openCodeHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{
			RenderedFileAction{Plan: RenderedFileInstallPlan{
				Path:        filepath.Join(openCodeConfigDir(), "plugins", openCodePluginName),
				Label:       "opencode plugin",
				ConfigLabel: "opencode plugin",
				Content: renderScriptTemplate(
					openCodePluginTemplate,
					openCodeIntegrationID,
					binary,
					openCodeIntegrationSource,
				),
				JSONContent: nil,
			}},
			ShimAction{},
		},
	}
}

func (openCodeHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{"opencode", openCodeSessionFlag, sessionID}
}

func openCodeConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG")); value != "" {
		return filepath.Dir(value)
	}

	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		return filepath.Join(value, "opencode")
	}

	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "opencode")
	}

	return filepath.Join(".config", "opencode")
}
