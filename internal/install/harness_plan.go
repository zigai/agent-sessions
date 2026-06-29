package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
)

func installHarnessAdapter(options Options) (Result, error) {
	adapter, ok := harnesspkg.Find(options.Harness)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
	}
	installer, ok := adapter.(harnesspkg.Installable)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
	}

	definition := adapter.Definition()
	plan := installer.InstallPlan(options.Binary)
	if options.UseShim && installPlanHasShim(plan) {
		return installShim(options, definition.ID)
	}

	for _, action := range plan.Actions {
		result, handled, err := installPlanAction(options, definition.ID, action)
		if handled {
			return result, err
		}
	}

	return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
}

func installPlanHasShim(plan harnesspkg.InstallPlan) bool {
	for _, action := range plan.Actions {
		if _, ok := action.(harnesspkg.ShimAction); ok {
			return true
		}
	}

	return false
}

func installPlanAction(options Options, harness registry.Harness, action harnesspkg.InstallAction) (Result, bool, error) {
	switch typed := action.(type) {
	case harnesspkg.JSONCommandHooksAction:
		result, err := installJSONCommandHooks(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.CursorJSONHooksAction:
		result, err := installCursorJSONHooks(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.ManagedTextBlockAction:
		result, err := installManagedTextBlock(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.RenderedFileAction:
		result, err := installRenderedPlan(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.RenderedFilesAction:
		result, err := installRenderedFilesPlan(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.PluginDirectoryAction:
		result, err := installPluginDirectory(options, harness, typed.Plan)

		return result, true, err
	case harnesspkg.ShimAction:
		var result Result

		return result, false, nil
	default:
		var result Result

		return result, false, nil
	}
}

func installJSONCommandHooks(
	options Options,
	harness registry.Harness,
	plan harnesspkg.JSONCommandHookInstallPlan,
) (Result, error) {
	label := installLabel(plan.Label, harness, "hooks")
	configLabel := installLabel(plan.ConfigLabel, harness, "config")

	return installJSONHookFile(options, jsonHookFileInstall{
		Harness:                 harness,
		Path:                    plan.Path,
		Apply:                   applyJSONCommandHooks(harness, plan),
		EncodeError:             "encoding " + label,
		CreateDirError:          "creating " + configLabel + " directory",
		WriteError:              "writing " + label,
		InstalledMessage:        label + " installed",
		AlreadyInstalledMessage: label + " already installed",
		DryRunMessage:           "dry run: " + label + " not written",
	})
}

func applyJSONCommandHooks(
	harness registry.Harness,
	plan harnesspkg.JSONCommandHookInstallPlan,
) func(map[string]any) bool {
	source := managedSource(plan.Source, harness)
	statusMessage := strings.TrimSpace(plan.StatusMessage)
	if !plan.OmitStatusMessage && statusMessage == "" {
		statusMessage = managedMarker
	}
	isManaged := isManagedSourceHookCommand(source)

	return func(config map[string]any) bool {
		changed := false
		for _, hook := range plan.Hooks {
			updated := upsertManagedCommandHookGroup(
				config,
				hook.Event,
				hook.Matcher,
				hook.Command,
				statusMessage,
				isManaged,
			)
			changed = changed || updated
		}

		return changed
	}
}

func installCursorJSONHooks(
	options Options,
	harness registry.Harness,
	plan harnesspkg.CursorJSONHookInstallPlan,
) (Result, error) {
	label := installLabel(plan.Label, harness, "hooks")
	configLabel := installLabel(plan.ConfigLabel, harness, "config")

	return installJSONHookFile(options, jsonHookFileInstall{
		Harness:                 harness,
		Path:                    plan.Path,
		Apply:                   applyCursorJSONHooks(harness, plan),
		EncodeError:             "encoding " + label,
		CreateDirError:          "creating " + configLabel + " directory",
		WriteError:              "writing " + label,
		InstalledMessage:        label + " installed",
		AlreadyInstalledMessage: label + " already installed",
		DryRunMessage:           "dry run: " + label + " not written",
	})
}

func applyCursorJSONHooks(
	harness registry.Harness,
	plan harnesspkg.CursorJSONHookInstallPlan,
) func(map[string]any) bool {
	source := managedSource(plan.Source, harness)
	isManaged := isManagedSourceHookCommand(source)

	return func(config map[string]any) bool {
		changed := ensureCursorVersion(config)
		for _, hook := range plan.Hooks {
			updated := upsertCursorHook(config, hook.Event, hook.Command, isManaged)
			changed = changed || updated
		}

		return changed
	}
}

func ensureCursorVersion(config map[string]any) bool {
	if _, ok := config["version"]; ok {
		return false
	}

	config["version"] = float64(1)

	return true
}

func upsertCursorHook(config map[string]any, event string, command string, isManaged func(string) bool) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		config["hooks"] = hooks
	}

	definitions, ok := hooks[event].([]any)
	if !ok {
		definitions = nil
	}

	managedCount, exactCount := countManagedCursorHooks(definitions, command, isManaged)
	if managedCount == 1 && exactCount == 1 {
		return false
	}

	definitions, _ = removeManagedCursorHooks(definitions, isManaged)
	hooks[event] = append(definitions, map[string]any{
		"command": command,
		"timeout": float64(harnesspkg.HookTimeoutSeconds),
	})

	return true
}

func countManagedCursorHooks(
	definitions []any,
	command string,
	isManaged func(string) bool,
) (int, int) {
	managedCount := 0
	exactCount := 0
	for _, definitionValue := range definitions {
		definition, ok := definitionValue.(map[string]any)
		if !ok {
			continue
		}
		hookCommand, commandOK := definition["command"].(string)
		if !commandOK || !isManaged(hookCommand) {
			continue
		}
		managedCount++
		if hookCommand == command {
			exactCount++
		}
	}

	return managedCount, exactCount
}

func removeManagedCursorHooks(definitions []any, isManaged func(string) bool) ([]any, bool) {
	cleanedDefinitions := make([]any, 0, len(definitions))
	removed := false
	for _, definitionValue := range definitions {
		definition, ok := definitionValue.(map[string]any)
		if !ok {
			cleanedDefinitions = append(cleanedDefinitions, definitionValue)
			continue
		}
		hookCommand, commandOK := definition["command"].(string)
		if commandOK && isManaged(hookCommand) {
			removed = true
			continue
		}

		cleanedDefinition := make(map[string]any, len(definition))
		maps.Copy(cleanedDefinition, definition)
		cleanedDefinitions = append(cleanedDefinitions, cleanedDefinition)
	}

	return cleanedDefinitions, removed
}

func installManagedTextBlock(
	options Options,
	harness registry.Harness,
	plan harnesspkg.ManagedTextBlockInstallPlan,
) (Result, error) {
	current, err := readTextFile(plan.Path)
	if err != nil {
		return Result{}, err
	}

	next := upsertManagedTextBlock(current, plan.StartMarker, plan.EndMarker, plan.Block)
	changed := current != next
	label := installLabel(plan.Label, harness, "hooks")
	configLabel := installLabel(plan.ConfigLabel, harness, "config")

	if err := writeInstallFile(
		plan.Path,
		[]byte(next),
		changed,
		options.DryRun,
		"creating "+configLabel+" directory",
		"writing "+label,
	); err != nil {
		return Result{}, err
	}

	return Result{
		Harness: string(harness),
		Path:    plan.Path,
		Changed: changed,
		Message: installMessage(label, changed, options.DryRun),
		Snippet: next,
		Error:   "",
	}, nil
}

func readTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	return string(data), nil
}

func upsertManagedTextBlock(current string, startMarker string, endMarker string, block string) string {
	cleaned := removeManagedTextBlock(current, startMarker, endMarker)

	return appendManagedTextBlock(cleaned, block)
}

func removeManagedTextBlock(current string, startMarker string, endMarker string) string {
	start := strings.Index(current, startMarker)
	if start < 0 {
		return current
	}

	endOffset := strings.Index(current[start:], endMarker)
	if endOffset < 0 {
		return current
	}

	end := start + endOffset + len(endMarker)
	for end < len(current) && (current[end] == '\r' || current[end] == '\n') {
		end++
	}

	before := strings.TrimRight(current[:start], " \t\r\n")
	after := strings.TrimLeft(current[end:], "\r\n")
	switch {
	case before == "":
		return after
	case after == "":
		return before
	default:
		return before + "\n\n" + after
	}
}

func appendManagedTextBlock(current string, block string) string {
	trimmed := strings.TrimRight(current, " \t\r\n")
	if trimmed == "" {
		return block
	}

	return trimmed + "\n\n" + block
}

func installRenderedPlan(
	options Options,
	harness registry.Harness,
	plan harnesspkg.RenderedFileInstallPlan,
) (Result, error) {
	content, err := renderInstallContent(plan.Content, plan.JSONContent)
	if err != nil {
		return Result{}, err
	}

	label := installLabel(plan.Label, harness, "artifact")
	configLabel := installLabel(plan.ConfigLabel, harness, "artifact")

	return installRenderedFile(options, renderedFileInstall{
		Harness:                 harness,
		Path:                    plan.Path,
		Content:                 content,
		CreateDirError:          "creating " + configLabel + " directory",
		WriteError:              "writing " + label,
		InstalledMessage:        label + " installed",
		AlreadyInstalledMessage: label + " already installed",
		DryRunMessage:           "dry run: " + label + " not written",
	})
}

func installRenderedFilesPlan(
	options Options,
	harness registry.Harness,
	plan harnesspkg.RenderedFilesInstallPlan,
) (Result, error) {
	files, err := renderRenderedFiles(plan.Files)
	if err != nil {
		return Result{}, err
	}

	label := installLabel(plan.Label, harness, "artifacts")
	configLabel := installLabel(plan.ConfigLabel, harness, "artifacts")

	changed, needsUpdate, err := renderedFilesNeedUpdate(plan.Dir, files, options.Force)
	if err != nil {
		return Result{}, err
	}
	if changed && !options.DryRun {
		for name, content := range files {
			path := filepath.Join(plan.Dir, name)
			err := writeInstallFile(
				path,
				[]byte(content),
				needsUpdate[name],
				false,
				"creating "+configLabel+" directory",
				"writing "+label,
			)
			if err != nil {
				return Result{}, err
			}
		}
	}

	return Result{
		Harness: string(harness),
		Path:    plan.Dir,
		Changed: changed,
		Message: installMessage(label, changed, options.DryRun),
		Snippet: renderedFilesSnippet(files, plan.SnippetOrder),
		Error:   "",
	}, nil
}

func renderInstallContent(content string, jsonContent any) (string, error) {
	if jsonContent == nil {
		return content, nil
	}

	data, err := json.MarshalIndent(jsonContent, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON install content: %w", err)
	}

	return string(append(data, '\n')), nil
}

func renderRenderedFiles(specs []harnesspkg.RenderedFileInstallSpec) (map[string]string, error) {
	files := make(map[string]string, len(specs))
	for _, spec := range specs {
		if err := validateInstallRelativePath(spec.Name); err != nil {
			return nil, err
		}
		content, err := renderInstallContent(spec.Content, spec.JSONContent)
		if err != nil {
			return nil, fmt.Errorf("encoding rendered file %s: %w", spec.Name, err)
		}
		if _, exists := files[spec.Name]; exists {
			return nil, fmt.Errorf("%w: %q", errDuplicatePluginFileName, spec.Name)
		}
		files[spec.Name] = content
	}

	return files, nil
}

func renderedFilesNeedUpdate(
	dir string,
	files map[string]string,
	force bool,
) (bool, map[string]bool, error) {
	changed := false
	needsUpdate := make(map[string]bool, len(files))
	for name, content := range files {
		path := filepath.Join(dir, name)
		update, err := fileNeedsUpdate(path, content, force)
		if err != nil {
			return false, nil, err
		}
		needsUpdate[name] = update
		changed = changed || update
	}

	return changed, needsUpdate, nil
}

func renderedFilesSnippet(files map[string]string, snippetOrder []string) string {
	order := snippetOrder
	if len(order) == 0 {
		order = make([]string, 0, len(files))
		for name := range files {
			order = append(order, name)
		}
		slices.Sort(order)
	}

	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, "== "+name+" ==\n"+files[name])
	}

	return strings.Join(parts, "\n")
}

func installPluginDirectory(
	options Options,
	harness registry.Harness,
	plan harnesspkg.PluginDirectoryInstallPlan,
) (Result, error) {
	files, err := renderPluginFiles(plan.Files)
	if err != nil {
		return Result{}, err
	}
	plugin := newPluginDirectoryInstall(plan, files)

	if err := plugin.ensureManaged(options.Force); err != nil {
		return Result{}, err
	}

	pluginChanged, err := plugin.needsUpdate()
	if err != nil {
		return Result{}, err
	}

	manifest, manifestChanged, err := plannedImportManifest(plan.ImportManifest, time.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	changed := pluginChanged || manifestChanged

	if changed && !options.DryRun {
		err := writePluginDirectoryChanges(plugin, plan.ImportManifest, manifest, pluginChanged, manifestChanged)
		if err != nil {
			return Result{}, err
		}
	}

	label := installLabel(plan.Label, harness, "plugin")

	return Result{
		Harness: string(harness),
		Path:    plugin.dir,
		Changed: changed,
		Message: installMessage(label, changed, options.DryRun),
		Snippet: plugin.snippet(),
		Error:   "",
	}, nil
}

func writePluginDirectoryChanges(
	plugin pluginDirectoryInstall,
	importPlan *harnesspkg.ImportManifestInstallPlan,
	manifest importManifest,
	pluginChanged bool,
	manifestChanged bool,
) error {
	var rollback func() error
	var commit func() error
	if pluginChanged {
		var err error
		rollback, commit, err = plugin.installStaged()
		if err != nil {
			return err
		}
	}

	if importPlan != nil && manifestChanged {
		if err := writeImportManifest(importPlan.Path, manifest); err != nil {
			return rollbackPluginDirectory(rollback, err)
		}
	}

	if commit == nil {
		return nil
	}

	return commit()
}

func rollbackPluginDirectory(rollback func() error, cause error) error {
	if rollback == nil {
		return cause
	}
	if err := rollback(); err != nil {
		return errors.Join(cause, fmt.Errorf("rolling back plugin directory: %w", err))
	}

	return cause
}

func plannedImportManifest(
	plan *harnesspkg.ImportManifestInstallPlan,
	now time.Time,
) (importManifest, bool, error) {
	if plan == nil {
		var manifest importManifest

		return manifest, false, nil
	}

	return importManifestWithPlan(*plan, now)
}

type importManifest struct {
	Imports []importEntry `json:"imports"`
}

type importEntry struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ImportedAt string   `json:"imported_at"`
	Components []string `json:"components"`
}

func importManifestWithPlan(plan harnesspkg.ImportManifestInstallPlan, now time.Time) (importManifest, bool, error) {
	manifest, err := readImportManifest(plan.Path)
	if err != nil {
		return importManifest{}, false, err
	}

	next, changed := upsertImport(manifest, plan, now)

	return next, changed, nil
}

func readImportManifest(path string) (importManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return importManifest{
				Imports: nil,
			}, nil
		}

		return importManifest{}, fmt.Errorf("reading import manifest: %w", err)
	}

	var manifest importManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return importManifest{}, fmt.Errorf("parsing import manifest: %w", err)
	}

	return manifest, nil
}

func upsertImport(
	manifest importManifest,
	plan harnesspkg.ImportManifestInstallPlan,
	now time.Time,
) (importManifest, bool) {
	for index, item := range manifest.Imports {
		if item.Name != plan.Name {
			continue
		}

		next := item
		if next.Source != plan.Source {
			next.Source = plan.Source
		}
		if next.ImportedAt == "" {
			next.ImportedAt = now.Format(time.RFC3339)
		}
		for _, component := range plan.Components {
			if !stringSliceContains(next.Components, component) {
				next.Components = append(next.Components, component)
			}
		}
		if importsEqual(item, next) {
			return manifest, false
		}

		manifest.Imports[index] = next
		return manifest, true
	}

	manifest.Imports = append(manifest.Imports, importEntry{
		Name:       plan.Name,
		Source:     plan.Source,
		ImportedAt: now.Format(time.RFC3339),
		Components: append([]string(nil), plan.Components...),
	})

	return manifest, true
}

func importsEqual(left importEntry, right importEntry) bool {
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

func writeImportManifest(path string, manifest importManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding import manifest: %w", err)
	}
	data = append(data, '\n')

	return writeFileAtomic(path, data, "creating config directory", "writing import manifest")
}

func writeFileAtomic(path string, data []byte, createDirError string, writeError string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%s: %w", createDirError, err)
	}

	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if keep {
			return
		}
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("%s: %w", writeError, err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("%s: %w", writeError, err)
	}
	keep = true

	return syncDir(dir)
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening directory: %w", err)
	}
	defer func() {
		_ = handle.Close()
	}()

	if err := handle.Sync(); err != nil {
		return fmt.Errorf("syncing directory: %w", err)
	}

	return nil
}

func stringSliceContains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func managedSource(source string, harness registry.Harness) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return string(harness) + "-hook"
	}

	return source
}

func isManagedSourceHookCommand(source string) func(string) bool {
	return func(command string) bool {
		return strings.Contains(command, "agent_sessions_integration="+source) ||
			strings.Contains(command, "--source "+source)
	}
}

func installLabel(value string, harness registry.Harness, suffix string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}

	return string(harness) + " " + suffix
}

func installMessage(label string, changed bool, dryRun bool) string {
	if dryRun {
		return "dry run: " + label + " not written"
	}
	if !changed {
		return label + " already installed"
	}

	return label + " installed"
}
