package harness

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	ompExtensionName       = "agent-sessions-state.ts"
	ompIntegrationID       = "omp"
	ompIntegrationSourceID = "omp-extension"
	ompSessionFlag         = "--session"
)

//go:embed assets/omp/agent-sessions-state.ts.tmpl
var ompExtensionTemplate string

type ompHarness struct {
	baseAdapter
}

func ompAdapter() Adapter {
	return ompHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessOmp, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (ompHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{RenderedFileAction{Plan: RenderedFileInstallPlan{
			Path:        filepath.Join(ompAgentDir(), "extensions", ompExtensionName),
			Label:       "oh-my-pi extension",
			ConfigLabel: "oh-my-pi extension",
			Content: renderScriptTemplate(
				ompExtensionTemplate,
				ompIntegrationID,
				binary,
				ompIntegrationSourceID,
			),
			JSONContent: nil,
		}}},
	}
}

func (ompHarness) ResumeCommand(sessionID string, sessionPath string) []string {
	if sessionPath != "" {
		return []string{ompIntegrationID, ompSessionFlag, sessionPath}
	}
	if sessionID != "" {
		return []string{ompIntegrationID, ompSessionFlag, sessionID}
	}

	return nil
}

func ompAgentDir() string {
	if value := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); value != "" {
		return value
	}

	configRoot := strings.TrimSpace(os.Getenv("PI_CONFIG_DIR"))
	if configRoot == "" {
		configRoot = ".omp"
	}
	if !filepath.IsAbs(configRoot) {
		if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
			configRoot = filepath.Join(home, configRoot)
		}
	}

	profile := strings.TrimSpace(os.Getenv("OMP_PROFILE"))
	if profile == "" {
		profile = strings.TrimSpace(os.Getenv("PI_PROFILE"))
	}
	if profile != "" && profile != "default" {
		configRoot = filepath.Join(configRoot, "profiles", profile)
	}

	return filepath.Join(configRoot, "agent")
}
