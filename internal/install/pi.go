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

	changed, err := fileNeedsUpdate(path, script, options.Force)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(dir, 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating pi extension directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, []byte(script), 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing pi extension: %w", writeErr)
		}
	}

	message := "pi extension installed"
	if !changed {
		message = "pi extension already installed"
	}

	if options.DryRun {
		message = "dry run: pi extension not written"
	}

	return Result{
		Harness: string(registry.HarnessPi),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: script,
		Error:   "",
	}, nil
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

type piExtensionTemplateData struct {
	ManagedMarker      string
	IntegrationID      string
	IntegrationVersion int
	Binary             string
	Source             string
}

func piExtension(binary string) (string, error) {
	rawTemplate, err := piAssets.ReadFile("assets/pi/agent-sessions-state.ts.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading pi extension template: %w", err)
	}

	parsedTemplate, err := template.New(piExtensionName).Parse(string(rawTemplate))
	if err != nil {
		return "", fmt.Errorf("parsing pi extension template: %w", err)
	}

	data := piExtensionTemplateData{
		ManagedMarker:      managedMarker,
		IntegrationID:      piIntegrationID,
		IntegrationVersion: piIntegrationVersion,
		Binary:             strconv.Quote(binary),
		Source:             strconv.Quote(piIntegrationSourceID),
	}

	var rendered bytes.Buffer

	executeErr := parsedTemplate.Execute(&rendered, data)
	if executeErr != nil {
		return "", fmt.Errorf("rendering pi extension template: %w", executeErr)
	}

	return rendered.String(), nil
}
