package install

import (
	"embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	openCodePluginName         = "agent-sessions-state.ts"
	openCodeIntegrationID      = "opencode"
	openCodeIntegrationVersion = 1
	openCodeIntegrationSource  = "opencode-plugin"
)

//go:embed assets/opencode/agent-sessions-state.ts.tmpl
var openCodeAssets embed.FS

func installOpenCode(options Options) (Result, error) {
	if options.UseShim {
		return installShim(options, registry.HarnessOpenCode)
	}

	dir := filepath.Join(openCodeConfigDir(), "plugins")
	path := filepath.Join(dir, openCodePluginName)
	script, err := openCodePlugin(options.Binary)
	if err != nil {
		return Result{}, err
	}

	return installRenderedFile(options, renderedFileInstall{
		Harness:                 registry.HarnessOpenCode,
		Path:                    path,
		Content:                 script,
		CreateDirError:          "creating opencode plugin directory",
		WriteError:              "writing opencode plugin",
		InstalledMessage:        "opencode plugin installed",
		AlreadyInstalledMessage: "opencode plugin already installed",
		DryRunMessage:           "dry run: opencode plugin not written",
	})
}

func openCodePlugin(binary string) (string, error) {
	return renderTemplateArtifact(templateArtifactOptions{
		Assets:             openCodeAssets,
		TemplatePath:       "assets/opencode/agent-sessions-state.ts.tmpl",
		TemplateName:       openCodePluginName,
		Label:              "opencode plugin",
		IntegrationID:      openCodeIntegrationID,
		IntegrationVersion: openCodeIntegrationVersion,
		Binary:             binary,
		Source:             openCodeIntegrationSource,
	})
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
