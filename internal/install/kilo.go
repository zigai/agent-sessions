package install

import (
	"embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	kiloPluginName         = "agent-sessions-state.ts"
	kiloIntegrationID      = "kilo"
	kiloIntegrationVersion = 1
	kiloIntegrationSource  = "kilo-plugin"
)

//go:embed assets/kilo/agent-sessions-state.ts.tmpl
var kiloAssets embed.FS

func installKilo(options Options) (Result, error) {
	dir := filepath.Join(kiloConfigDir(), "plugin")
	path := filepath.Join(dir, kiloPluginName)
	script, err := kiloPlugin(options.Binary)
	if err != nil {
		return Result{}, err
	}

	return installRenderedFile(options, renderedFileInstall{
		Harness:                 registry.HarnessKilo,
		Path:                    path,
		Content:                 script,
		CreateDirError:          "creating kilo plugin directory",
		WriteError:              "writing kilo plugin",
		InstalledMessage:        "kilo plugin installed",
		AlreadyInstalledMessage: "kilo plugin already installed",
		DryRunMessage:           "dry run: kilo plugin not written",
	})
}

func kiloPlugin(binary string) (string, error) {
	return renderTemplateArtifact(templateArtifactOptions{
		Assets:             kiloAssets,
		TemplatePath:       "assets/kilo/agent-sessions-state.ts.tmpl",
		TemplateName:       kiloPluginName,
		Label:              "kilo plugin",
		IntegrationID:      kiloIntegrationID,
		IntegrationVersion: kiloIntegrationVersion,
		Binary:             binary,
		Source:             kiloIntegrationSource,
	})
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
