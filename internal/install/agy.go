package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	agyPluginName         = "agent-sessions-state"
	agyMarkerFileName     = ".agent-sessions-managed"
	agyImportManifestName = "import_manifest.json"
	agyImportSource       = "antigravity"
	agyImportComponent    = "hooks"
	agyIntegrationID      = "agy"
	agyIntegrationVersion = 1
	agyIntegrationSource  = "agy-hook"
)

func installAgy(options Options) (Result, error) {
	configDir := agyConfigDir()
	dir := filepath.Join(configDir, "plugins", agyPluginName)
	manifestPath := filepath.Join(configDir, agyImportManifestName)
	files, err := agyPluginFiles(options.Binary)
	if err != nil {
		return Result{}, err
	}

	if guardErr := ensureAgyPluginManaged(dir, options.Force); guardErr != nil {
		return Result{}, guardErr
	}

	changed, err := agyPluginNeedsUpdate(dir, files)
	if err != nil {
		return Result{}, err
	}

	manifest, manifestChanged, err := agyImportManifestWithPlugin(manifestPath, time.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	changed = changed || manifestChanged

	if changed && !options.DryRun {
		if writeErr := writeAgyPluginFiles(dir, files); writeErr != nil {
			return Result{}, writeErr
		}
		if manifestChanged {
			if writeErr := writeAgyImportManifest(manifestPath, manifest); writeErr != nil {
				return Result{}, writeErr
			}
		}
	}

	return Result{
		Harness: string(registry.HarnessAgy),
		Path:    dir,
		Changed: changed,
		Message: agyInstallMessage(changed, options.DryRun),
		Snippet: agyPluginSnippet(files),
		Error:   "",
	}, nil
}

type agyImportManifest struct {
	Imports []agyImport `json:"imports"`
}

type agyImport struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ImportedAt string   `json:"imported_at"`
	Components []string `json:"components"`
}

func agyImportManifestWithPlugin(path string, now time.Time) (agyImportManifest, bool, error) {
	manifest, err := readAgyImportManifest(path)
	if err != nil {
		return agyImportManifest{}, false, err
	}

	next, changed := upsertAgyImport(manifest, now)

	return next, changed, nil
}

func readAgyImportManifest(path string) (agyImportManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agyImportManifest{
				Imports: nil,
			}, nil
		}

		return agyImportManifest{}, fmt.Errorf("reading agy import manifest: %w", err)
	}

	var manifest agyImportManifest
	if unmarshalErr := json.Unmarshal(data, &manifest); unmarshalErr != nil {
		return agyImportManifest{}, fmt.Errorf("parsing agy import manifest: %w", unmarshalErr)
	}

	return manifest, nil
}

func upsertAgyImport(manifest agyImportManifest, now time.Time) (agyImportManifest, bool) {
	for index, item := range manifest.Imports {
		if item.Name != agyPluginName {
			continue
		}

		next := item
		if next.Source != agyImportSource {
			next.Source = agyImportSource
		}
		if next.ImportedAt == "" {
			next.ImportedAt = now.Format(time.RFC3339)
		}
		if !stringSliceContains(next.Components, agyImportComponent) {
			next.Components = append(next.Components, agyImportComponent)
		}
		if agyImportsEqual(item, next) {
			return manifest, false
		}

		manifest.Imports[index] = next
		return manifest, true
	}

	manifest.Imports = append(manifest.Imports, agyImport{
		Name:       agyPluginName,
		Source:     agyImportSource,
		ImportedAt: now.Format(time.RFC3339),
		Components: []string{agyImportComponent},
	})

	return manifest, true
}

func agyImportsEqual(left agyImport, right agyImport) bool {
	if left.Name != right.Name || left.Source != right.Source || left.ImportedAt != right.ImportedAt {
		return false
	}

	if len(left.Components) != len(right.Components) {
		return false
	}
	for index := range left.Components {
		if left.Components[index] != right.Components[index] {
			return false
		}
	}

	return true
}

func writeAgyImportManifest(path string, manifest agyImportManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding agy import manifest: %w", err)
	}
	data = append(data, '\n')

	mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700)
	if mkdirErr != nil {
		return fmt.Errorf("creating agy config directory: %w", mkdirErr)
	}
	writeErr := os.WriteFile(path, data, 0o600)
	if writeErr != nil {
		return fmt.Errorf("writing agy import manifest: %w", writeErr)
	}

	return nil
}

func ensureAgyPluginManaged(dir string, force bool) error {
	managed, err := agyPluginManaged(dir)
	if err != nil {
		return err
	}
	if !managed && !force {
		return fmt.Errorf("%w: %s; pass --force to replace it", errForeignFile, dir)
	}

	return nil
}

func writeAgyPluginFiles(dir string, files map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating agy plugin directory: %w", err)
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing agy plugin file %s: %w", path, err)
		}
	}

	return nil
}

func agyInstallMessage(changed bool, dryRun bool) string {
	if dryRun {
		return "dry run: agy plugin not written"
	}
	if !changed {
		return "agy plugin already installed"
	}

	return "agy plugin installed"
}

func agyPluginFiles(binary string) (map[string]string, error) {
	manifest, err := json.MarshalIndent(map[string]any{
		"name": agyPluginName,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding agy plugin manifest: %w", err)
	}

	hooks, err := json.MarshalIndent(agyHookConfig(binary), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding agy hooks: %w", err)
	}

	return map[string]string{
		"plugin.json":     string(append(manifest, '\n')),
		"hooks.json":      string(append(hooks, '\n')),
		agyMarkerFileName: agyMarkerContent(),
	}, nil
}

func agyHookConfig(binary string) map[string]any {
	return map[string]any{
		agyPluginName: map[string]any{
			"PreInvocation":  []any{agyHookHandler(binary, "PreInvocation")},
			"PostInvocation": []any{agyHookHandler(binary, "PostInvocation")},
			"PreToolUse":     []any{agyToolHookGroup(binary, "PreToolUse")},
			"PostToolUse":    []any{agyToolHookGroup(binary, "PostToolUse")},
			hookEventStop:    []any{agyHookHandler(binary, hookEventStop)},
		},
	}
}

func agyToolHookGroup(binary string, event string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{
			agyHookHandler(binary, event),
		},
	}
}

func agyHookHandler(binary string, event string) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": agyHookCommand(binary, event),
		"timeout": float64(hookTimeoutSeconds),
	}
}

func agyHookCommand(binary string, event string) string {
	return strings.Join([]string{
		shellQuote(binary),
		"agy-hook",
		"--event", shellQuote(event),
	}, " ")
}

func agyPluginManaged(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking agy plugin directory: %w", err)
	}
	if !info.IsDir() {
		return false, nil
	}

	marker, err := os.ReadFile(filepath.Join(dir, agyMarkerFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("reading agy plugin marker: %w", err)
	}

	return strings.Contains(string(marker), managedMarker), nil
}

func agyPluginNeedsUpdate(dir string, files map[string]string) (bool, error) {
	for name, content := range files {
		path := filepath.Join(dir, name)
		current, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}

			return false, fmt.Errorf("reading agy plugin file %s: %w", path, err)
		}
		if string(current) != content {
			return true, nil
		}
	}

	return false, nil
}

func agyPluginSnippet(files map[string]string) string {
	names := []string{"plugin.json", "hooks.json", agyMarkerFileName}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, "== "+name+" ==\n"+files[name])
	}

	return strings.Join(parts, "\n")
}

func agyMarkerContent() string {
	return strings.Join([]string{
		managedMarker,
		"AGENT_SESSIONS_INTEGRATION_ID=" + agyIntegrationID,
		"AGENT_SESSIONS_INTEGRATION_VERSION=" + strconv.Itoa(agyIntegrationVersion),
		"AGENT_SESSIONS_SOURCE=" + agyIntegrationSource,
		"",
	}, "\n")
}

func stringSliceContains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func agyConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("AGY_CONFIG_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".gemini", "config")
	}

	return filepath.Join(".gemini", "config")
}
