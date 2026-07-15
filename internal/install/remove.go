package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
)

var errRemoveFailed = errors.New("one or more integrations failed to remove")

// Remove deletes only artifacts owned by agent-sessions for one harness.
func Remove(options Options) (Result, error) {
	adapter, ok := harnesspkg.Find(options.Harness)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
	}
	installer, ok := adapter.(harnesspkg.Installable)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
	}
	if options.Binary == "" {
		options.Binary = defaultBinary
	}
	shimPath := filepath.Join(registry.DefaultStateDir(), "shims", string(options.Harness))
	shimStatus, err := ClassifyArtifact(shimPath)
	if err != nil {
		return Result{}, err
	}
	if shimStatus == ArtifactForeign {
		return Result{}, fmt.Errorf("%w: %s", errForeignFile, shimPath)
	}
	result, err := removeNativeIntegration(options, installer.InstallPlan(options.Binary))
	if err != nil {
		return result, err
	}
	shimChanged, err := removeOwnedShim(shimPath, options.DryRun, shimStatus)
	if err != nil {
		return Result{}, err
	}
	if shimChanged {
		if !result.Changed {
			result.Path = shimPath
		}
		result.Changed = true
		result.Message = removeResult(options.Harness, result.Path, true, options.DryRun).Message
	}
	return result, nil
}

func removeNativeIntegration(options Options, plan harnesspkg.InstallPlan) (Result, error) {
	for _, action := range plan.Actions {
		result, handled, err := removePlanAction(options, options.Harness, action)
		if handled {
			return result, err
		}
	}
	return emptyResult(), fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
}

func removeOwnedShim(path string, dryRun bool, status ArtifactStatus) (bool, error) {
	if status == ArtifactMissing {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("removing managed shim %s: %w", path, err)
	}
	return true, nil
}

// RemoveAll deletes owned artifacts for every installable harness.
func RemoveAll(options Options) ([]Result, error) {
	results := make([]Result, 0, len(AllHarnesses))
	failures := make([]string, 0)
	for _, harnessID := range AllHarnesses {
		next := options
		next.Harness = harnessID
		result, err := Remove(next)
		if err != nil {
			result = Result{Harness: string(harnessID), Path: "", Changed: false, Message: "remove failed", Snippet: "", Error: err.Error()}
			failures = append(failures, string(harnessID))
		}
		results = append(results, result)
	}
	if len(failures) > 0 {
		return results, fmt.Errorf("%w: %s", errRemoveFailed, strings.Join(failures, ", "))
	}
	return results, nil
}

func removePlanAction(options Options, harnessID registry.Harness, action harnesspkg.InstallAction) (Result, bool, error) {
	switch typed := action.(type) {
	case harnesspkg.JSONCommandHooksAction:
		result, err := removeJSONCommandHooks(options, harnessID, typed.Plan)
		return result, true, err
	case harnesspkg.CursorJSONHooksAction:
		result, err := removeCursorJSONHooks(options, harnessID, typed.Plan)
		return result, true, err
	case harnesspkg.ManagedTextBlockAction:
		result, err := removeTextBlock(options, harnessID, typed.Plan)
		return result, true, err
	case harnesspkg.RenderedFileAction:
		result, err := removeOwnedFiles(options, harnessID, []string{typed.Plan.Path}, typed.Plan.Path)
		return result, true, err
	case harnesspkg.RenderedFilesAction:
		paths := make([]string, 0, len(typed.Plan.Files))
		for _, file := range typed.Plan.Files {
			paths = append(paths, filepath.Join(typed.Plan.Dir, file.Name))
		}
		result, err := removeOwnedFiles(options, harnessID, paths, typed.Plan.Dir)
		return result, true, err
	case harnesspkg.PluginDirectoryAction:
		result, err := removePluginDirectory(options, harnessID, typed.Plan)
		return result, true, err
	case harnesspkg.ShimAction:
		return emptyResult(), false, nil
	default:
		return emptyResult(), false, nil
	}
}

func removeJSONCommandHooks(options Options, harnessID registry.Harness, plan harnesspkg.JSONCommandHookInstallPlan) (Result, error) {
	return removeJSONHooks(options, harnessID, plan.Path, func(config map[string]any) bool {
		hooks, ok := config["hooks"].(map[string]any)
		if !ok {
			return false
		}
		isManaged := isManagedSourceHookCommand(managedSource(plan.Source, harnessID))
		changed := false
		for _, spec := range plan.Hooks {
			groups, ok := hooks[spec.Event].([]any)
			if !ok {
				continue
			}
			cleaned, removed := removeManagedCommandHookGroups(groups, isManaged)
			if !removed {
				continue
			}
			changed = true
			if len(cleaned) == 0 {
				delete(hooks, spec.Event)
			} else {
				hooks[spec.Event] = cleaned
			}
		}
		return changed
	})
}

func removeCursorJSONHooks(options Options, harnessID registry.Harness, plan harnesspkg.CursorJSONHookInstallPlan) (Result, error) {
	return removeJSONHooks(options, harnessID, plan.Path, func(config map[string]any) bool {
		hooks, ok := config["hooks"].(map[string]any)
		if !ok {
			return false
		}
		isManaged := isManagedSourceHookCommand(managedSource(plan.Source, harnessID))
		changed := false
		for _, spec := range plan.Hooks {
			definitions, ok := hooks[spec.Event].([]any)
			if !ok {
				continue
			}
			cleaned, removed := removeManagedCursorHooks(definitions, isManaged)
			if !removed {
				continue
			}
			changed = true
			if len(cleaned) == 0 {
				delete(hooks, spec.Event)
			} else {
				hooks[spec.Event] = cleaned
			}
		}
		return changed
	})
}

func removeJSONHooks(options Options, harnessID registry.Harness, path string, apply func(map[string]any) bool) (Result, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return removeResult(harnessID, path, false, options.DryRun), nil
	} else if err != nil {
		return Result{}, fmt.Errorf("checking %s: %w", path, err)
	}
	config, err := readJSONObject(path)
	if err != nil {
		return Result{}, err
	}
	changed := apply(config)
	if changed && !options.DryRun {
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return Result{}, fmt.Errorf("encoding cleaned integration config: %w", err)
		}
		if err := writeFileAtomic(path, append(data, '\n'), "creating config directory", "writing cleaned integration config"); err != nil {
			return Result{}, err
		}
	}
	return removeResult(harnessID, path, changed, options.DryRun), nil
}

func removeTextBlock(options Options, harnessID registry.Harness, plan harnesspkg.ManagedTextBlockInstallPlan) (Result, error) {
	current, err := readTextFile(plan.Path)
	if err != nil {
		return Result{}, err
	}
	next := removeManagedTextBlock(current, plan.StartMarker, plan.EndMarker)
	changed := next != current
	if changed && !options.DryRun {
		if err := writeFileAtomic(plan.Path, []byte(next), "creating config directory", "writing cleaned config"); err != nil {
			return Result{}, err
		}
	}
	return removeResult(harnessID, plan.Path, changed, options.DryRun), nil
}

func removeOwnedFiles(options Options, harnessID registry.Harness, paths []string, resultPath string) (Result, error) {
	managed := make([]string, 0, len(paths))
	for _, path := range paths {
		status, err := ClassifyArtifact(path)
		if err != nil {
			return Result{}, err
		}
		switch status {
		case ArtifactMissing:
			continue
		case ArtifactForeign:
			return Result{}, fmt.Errorf("%w: %s", errForeignFile, path)
		case ArtifactCurrent, ArtifactStale:
			managed = append(managed, path)
		}
	}
	if !options.DryRun {
		for _, path := range managed {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return Result{}, fmt.Errorf("removing managed integration %s: %w", path, err)
			}
		}
	}
	return removeResult(harnessID, resultPath, len(managed) > 0, options.DryRun), nil
}

func removePluginDirectory(options Options, harnessID registry.Harness, plan harnesspkg.PluginDirectoryInstallPlan) (Result, error) {
	plugin := newPluginDirectoryInstall(plan, nil)
	managed, err := plugin.managed()
	if err != nil {
		return Result{}, err
	}
	_, statErr := os.Stat(plan.Dir)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Result{}, fmt.Errorf("checking plugin directory: %w", statErr)
	}
	if exists && !managed {
		return Result{}, fmt.Errorf("%w: %s", errForeignFile, plan.Dir)
	}
	manifestChanged := false
	var manifest importManifest
	if plan.ImportManifest != nil {
		manifest, err = readImportManifest(plan.ImportManifest.Path)
		if err != nil {
			return Result{}, err
		}
		manifest, manifestChanged = removeImport(manifest, plan.ImportManifest.Name)
	}
	changed := exists || manifestChanged
	if changed && !options.DryRun {
		if err := applyPluginRemoval(plan, exists, manifestChanged, manifest); err != nil {
			return Result{}, err
		}
	}
	return removeResult(harnessID, plan.Dir, changed, options.DryRun), nil
}

func applyPluginRemoval(plan harnesspkg.PluginDirectoryInstallPlan, exists bool, manifestChanged bool, manifest importManifest) error {
	if exists {
		if err := os.RemoveAll(plan.Dir); err != nil {
			return fmt.Errorf("removing managed plugin %s: %w", plan.Dir, err)
		}
	}
	if plan.ImportManifest != nil && manifestChanged {
		if err := writeImportManifest(plan.ImportManifest.Path, manifest); err != nil {
			return err
		}
	}
	return nil
}

func removeImport(manifest importManifest, name string) (importManifest, bool) {
	next := make([]importEntry, 0, len(manifest.Imports))
	removed := false
	for _, item := range manifest.Imports {
		if item.Name == name {
			removed = true
			continue
		}
		next = append(next, item)
	}
	manifest.Imports = next
	return manifest, removed
}

func removeResult(harnessID registry.Harness, path string, changed bool, dryRun bool) Result {
	message := "integration is not installed"
	if changed && dryRun {
		message = "would remove integration"
	} else if changed {
		message = "integration removed"
	}
	return Result{Harness: string(harnessID), Path: path, Changed: changed, Message: message, Snippet: "", Error: ""}
}

func emptyResult() Result {
	return Result{Harness: "", Path: "", Changed: false, Message: "", Snippet: "", Error: ""}
}
