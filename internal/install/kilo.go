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

	changed, err := fileNeedsUpdate(path, script, options.Force)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(dir, 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating kilo plugin directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, []byte(script), 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing kilo plugin: %w", writeErr)
		}
	}

	message := "kilo plugin installed"
	if !changed {
		message = "kilo plugin already installed"
	}

	if options.DryRun {
		message = "dry run: kilo plugin not written"
	}

	return Result{
		Harness: string(registry.HarnessKilo),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: script,
		Error:   "",
	}, nil
}

type kiloPluginTemplateData struct {
	ManagedMarker      string
	IntegrationID      string
	IntegrationVersion int
	Binary             string
	Source             string
}

func kiloPlugin(binary string) (string, error) {
	rawTemplate, err := kiloAssets.ReadFile("assets/kilo/agent-sessions-state.ts.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading kilo plugin template: %w", err)
	}

	parsedTemplate, err := template.New(kiloPluginName).Parse(string(rawTemplate))
	if err != nil {
		return "", fmt.Errorf("parsing kilo plugin template: %w", err)
	}

	data := kiloPluginTemplateData{
		ManagedMarker:      managedMarker,
		IntegrationID:      kiloIntegrationID,
		IntegrationVersion: kiloIntegrationVersion,
		Binary:             strconv.Quote(binary),
		Source:             strconv.Quote(kiloIntegrationSource),
	}

	var rendered bytes.Buffer

	executeErr := parsedTemplate.Execute(&rendered, data)
	if executeErr != nil {
		return "", fmt.Errorf("rendering kilo plugin template: %w", executeErr)
	}

	return rendered.String(), nil
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
