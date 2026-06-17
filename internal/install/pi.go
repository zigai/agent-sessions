package install

import (
	"embed"
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
)

//go:embed assets/pi/agent-sessions-state.ts.tmpl
var piAssets embed.FS

func installPi(options Options) (Result, error) {
	dir := filepath.Join(piAgentDir(), "extensions")
	path := filepath.Join(dir, piExtensionName)
	script, err := piExtension(options.Binary)
	if err != nil {
		return Result{}, err
	}

	return installRenderedFile(options, renderedFileInstall{
		Harness:                 registry.HarnessPi,
		Path:                    path,
		Content:                 script,
		CreateDirError:          "creating pi extension directory",
		WriteError:              "writing pi extension",
		InstalledMessage:        "pi extension installed",
		AlreadyInstalledMessage: "pi extension already installed",
		DryRunMessage:           "dry run: pi extension not written",
	})
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

func piExtension(binary string) (string, error) {
	return renderTemplateArtifact(templateArtifactOptions{
		Assets:             piAssets,
		TemplatePath:       "assets/pi/agent-sessions-state.ts.tmpl",
		TemplateName:       piExtensionName,
		Label:              "pi extension",
		IntegrationID:      piIntegrationID,
		IntegrationVersion: piIntegrationVersion,
		Binary:             binary,
		Source:             piIntegrationSourceID,
	})
}
