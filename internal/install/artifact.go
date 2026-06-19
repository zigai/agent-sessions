package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"hooks": map[string]any{}}, nil
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"hooks": map[string]any{}}, nil
	}

	var config map[string]any
	unmarshalErr := json.Unmarshal(data, &config)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, unmarshalErr)
	}
	if config == nil {
		config = map[string]any{"hooks": map[string]any{}}
	}

	return config, nil
}
