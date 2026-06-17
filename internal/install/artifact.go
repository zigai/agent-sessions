package install

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/template"

	"github.com/zigai/agent-sessions/pkg/registry"
)

type renderedFileInstall struct {
	Harness                 registry.Harness
	Path                    string
	Content                 string
	CreateDirError          string
	WriteError              string
	InstalledMessage        string
	AlreadyInstalledMessage string
	DryRunMessage           string
}

func installRenderedFile(options Options, file renderedFileInstall) (Result, error) {
	changed, err := fileNeedsUpdate(file.Path, file.Content, options.Force)
	if err != nil {
		return Result{}, err
	}

	if writeErr := writeInstallFile(file.Path, []byte(file.Content), changed, options.DryRun, file.CreateDirError, file.WriteError); writeErr != nil {
		return Result{}, writeErr
	}

	return Result{
		Harness: string(file.Harness),
		Path:    file.Path,
		Changed: changed,
		Message: renderedFileMessage(changed, options.DryRun, file),
		Snippet: file.Content,
		Error:   "",
	}, nil
}

func renderedFileMessage(changed bool, dryRun bool, file renderedFileInstall) string {
	if dryRun {
		return file.DryRunMessage
	}
	if !changed {
		return file.AlreadyInstalledMessage
	}

	return file.InstalledMessage
}

type jsonHookFileInstall struct {
	Harness                 registry.Harness
	Path                    string
	Apply                   func(map[string]any) bool
	EncodeError             string
	CreateDirError          string
	WriteError              string
	InstalledMessage        string
	AlreadyInstalledMessage string
	DryRunMessage           string
}

func installJSONHookFile(options Options, file jsonHookFileInstall) (Result, error) {
	config, err := readJSONObject(file.Path)
	if err != nil {
		return Result{}, err
	}

	changed := file.Apply(config)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", file.EncodeError, err)
	}
	data = append(data, '\n')

	if writeErr := writeInstallFile(file.Path, data, changed, options.DryRun, file.CreateDirError, file.WriteError); writeErr != nil {
		return Result{}, writeErr
	}

	return Result{
		Harness: string(file.Harness),
		Path:    file.Path,
		Changed: changed,
		Message: jsonHookFileMessage(changed, options.DryRun, file),
		Snippet: string(data),
		Error:   "",
	}, nil
}

func jsonHookFileMessage(changed bool, dryRun bool, file jsonHookFileInstall) string {
	if dryRun {
		return file.DryRunMessage
	}
	if !changed {
		return file.AlreadyInstalledMessage
	}

	return file.InstalledMessage
}

func writeInstallFile(path string, data []byte, changed bool, dryRun bool, createDirError string, writeError string) error {
	if !changed || dryRun {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%s: %w", createDirError, err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("%s: %w", writeError, err)
	}

	return nil
}

type templateArtifactOptions struct {
	Assets             embed.FS
	TemplatePath       string
	TemplateName       string
	Label              string
	IntegrationID      string
	IntegrationVersion int
	Binary             string
	Source             string
}

type templateArtifactData struct {
	ManagedMarker      string
	IntegrationID      string
	IntegrationVersion int
	Binary             string
	Source             string
}

func renderTemplateArtifact(options templateArtifactOptions) (string, error) {
	rawTemplate, err := options.Assets.ReadFile(options.TemplatePath)
	if err != nil {
		return "", fmt.Errorf("reading %s template: %w", options.Label, err)
	}

	parsedTemplate, err := template.New(options.TemplateName).Parse(string(rawTemplate))
	if err != nil {
		return "", fmt.Errorf("parsing %s template: %w", options.Label, err)
	}

	data := templateArtifactData{
		ManagedMarker:      managedMarker,
		IntegrationID:      options.IntegrationID,
		IntegrationVersion: options.IntegrationVersion,
		Binary:             strconv.Quote(options.Binary),
		Source:             strconv.Quote(options.Source),
	}

	var rendered bytes.Buffer
	if executeErr := parsedTemplate.Execute(&rendered, data); executeErr != nil {
		return "", fmt.Errorf("rendering %s template: %w", options.Label, executeErr)
	}

	return rendered.String(), nil
}
