package install

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

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

	changed, err := fileNeedsUpdate(path, script, options.Force)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(dir, 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating opencode plugin directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, []byte(script), 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing opencode plugin: %w", writeErr)
		}
	}

	message := "opencode plugin installed"
	if !changed {
		message = "opencode plugin already installed"
	}

	if options.DryRun {
		message = "dry run: opencode plugin not written"
	}

	return Result{
		Harness: string(registry.HarnessOpenCode),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: script,
		Error:   "",
	}, nil
}

type openCodePluginTemplateData struct {
	ManagedMarker      string
	IntegrationID      string
	IntegrationVersion int
	Binary             string
	Source             string
}

func openCodePlugin(binary string) (string, error) {
	rawTemplate, err := openCodeAssets.ReadFile("assets/opencode/agent-sessions-state.ts.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading opencode plugin template: %w", err)
	}

	parsedTemplate, err := template.New(openCodePluginName).Parse(string(rawTemplate))
	if err != nil {
		return "", fmt.Errorf("parsing opencode plugin template: %w", err)
	}

	data := openCodePluginTemplateData{
		ManagedMarker:      managedMarker,
		IntegrationID:      openCodeIntegrationID,
		IntegrationVersion: openCodeIntegrationVersion,
		Binary:             strconv.Quote(binary),
		Source:             strconv.Quote(openCodeIntegrationSource),
	}

	var rendered bytes.Buffer

	executeErr := parsedTemplate.Execute(&rendered, data)
	if executeErr != nil {
		return "", fmt.Errorf("rendering opencode plugin template: %w", executeErr)
	}

	return rendered.String(), nil
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
