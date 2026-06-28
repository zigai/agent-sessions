package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
)

var (
	errDuplicatePluginFileName = errors.New("duplicate plugin file name")
	errInvalidPluginFileName   = errors.New("invalid plugin file name")
)

func renderPluginFiles(specs []harnesspkg.PluginFileInstallSpec) (map[string]string, error) {
	files := make(map[string]string, len(specs))
	for _, spec := range specs {
		if err := validatePluginFileName(spec.Name); err != nil {
			return nil, err
		}
		content, err := renderInstallContent(spec.Content, spec.JSONContent)
		if err != nil {
			return nil, fmt.Errorf("encoding plugin file %s: %w", spec.Name, err)
		}
		if _, exists := files[spec.Name]; exists {
			return nil, fmt.Errorf("%w: %q", errDuplicatePluginFileName, spec.Name)
		}
		files[spec.Name] = content
	}

	return files, nil
}

func validatePluginFileName(name string) error {
	if strings.TrimSpace(name) == "" || filepath.Base(name) != name {
		return fmt.Errorf("%w: %q", errInvalidPluginFileName, name)
	}

	return nil
}

type pluginDirectoryInstall struct {
	dir          string
	markerFile   string
	files        map[string]string
	snippetOrder []string
}

func newPluginDirectoryInstall(
	plan harnesspkg.PluginDirectoryInstallPlan,
	files map[string]string,
) pluginDirectoryInstall {
	return pluginDirectoryInstall{
		dir:          plan.Dir,
		markerFile:   plan.MarkerFile,
		files:        files,
		snippetOrder: plan.SnippetOrder,
	}
}

func (plugin pluginDirectoryInstall) ensureManaged(force bool) error {
	managed, err := plugin.managed()
	if err != nil {
		return err
	}
	if !managed && !force {
		return fmt.Errorf("%w: %s; pass --force to replace it", errForeignFile, plugin.dir)
	}

	return nil
}

func (plugin pluginDirectoryInstall) managed() (bool, error) {
	info, statErr := os.Stat(plugin.dir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking plugin directory: %w", statErr)
	}
	if !info.IsDir() {
		return false, nil
	}
	if plugin.markerFile == "" {
		return false, nil
	}

	marker, readErr := os.ReadFile(filepath.Join(plugin.dir, plugin.markerFile))
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("reading plugin marker: %w", readErr)
	}

	return strings.Contains(string(marker), managedMarker), nil
}

func (plugin pluginDirectoryInstall) needsUpdate() (bool, error) {
	info, statErr := os.Stat(plugin.dir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking plugin directory %s: %w", plugin.dir, statErr)
	}
	if !info.IsDir() {
		return true, nil
	}

	for name, content := range plugin.files {
		path := filepath.Join(plugin.dir, name)
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return true, nil
			}

			return false, fmt.Errorf("reading plugin file %s: %w", path, readErr)
		}
		if string(current) != content {
			return true, nil
		}
	}

	entries, readDirErr := os.ReadDir(plugin.dir)
	if readDirErr != nil {
		if errors.Is(readDirErr, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("reading plugin directory %s: %w", plugin.dir, readDirErr)
	}
	for _, entry := range entries {
		if _, ok := plugin.files[entry.Name()]; !ok {
			return true, nil
		}
	}

	return false, nil
}

func (plugin pluginDirectoryInstall) stage() (string, error) {
	parent := filepath.Dir(plugin.dir)
	if mkdirErr := os.MkdirAll(parent, 0o700); mkdirErr != nil {
		return "", fmt.Errorf("creating plugin parent directory: %w", mkdirErr)
	}

	stagedDir, stageErr := os.MkdirTemp(parent, "."+filepath.Base(plugin.dir)+".stage-*")
	if stageErr != nil {
		return "", fmt.Errorf("creating staged plugin directory: %w", stageErr)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stagedDir)
		}
	}()

	for name, content := range plugin.files {
		path := filepath.Join(stagedDir, name)
		if writeErr := writeStagedPluginFile(path, []byte(content)); writeErr != nil {
			return "", fmt.Errorf("writing plugin file %s: %w", path, writeErr)
		}
	}
	if syncErr := syncDir(stagedDir); syncErr != nil {
		return "", syncErr
	}
	keep = true

	return stagedDir, nil
}

func writeStagedPluginFile(path string, data []byte) error {
	file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if openErr != nil {
		return fmt.Errorf("opening staged plugin file: %w", openErr)
	}
	keepOpen := true
	defer func() {
		if keepOpen {
			_ = file.Close()
		}
	}()

	if _, writeErr := file.Write(data); writeErr != nil {
		return fmt.Errorf("writing staged plugin file: %w", writeErr)
	}
	if syncErr := file.Sync(); syncErr != nil {
		return fmt.Errorf("syncing staged plugin file: %w", syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("closing staged plugin file: %w", closeErr)
	}
	keepOpen = false

	return nil
}

func (plugin pluginDirectoryInstall) installStaged() (func() error, func() error, error) {
	stagedDir, stageErr := plugin.stage()
	if stageErr != nil {
		return nil, nil, stageErr
	}
	defer func() {
		_ = os.RemoveAll(stagedDir)
	}()

	return plugin.replace(stagedDir)
}

// replace installs a fully staged plugin directory and returns
// rollback/commit callbacks so related manifest updates can stay all-or-restore
// at the install operation level.
func (plugin pluginDirectoryInstall) replace(stagedDir string) (func() error, func() error, error) {
	parent := filepath.Dir(plugin.dir)
	backup, backupErr := os.MkdirTemp(parent, "."+filepath.Base(plugin.dir)+".backup-*")
	if backupErr != nil {
		return nil, nil, fmt.Errorf("creating plugin backup path: %w", backupErr)
	}
	if removeErr := os.Remove(backup); removeErr != nil {
		return nil, nil, fmt.Errorf("preparing plugin backup path: %w", removeErr)
	}

	backupExists := false
	if renameErr := os.Rename(plugin.dir, backup); renameErr != nil {
		if !errors.Is(renameErr, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("backing up plugin directory: %w", renameErr)
		}
	} else {
		backupExists = true
	}

	if renameErr := os.Rename(stagedDir, plugin.dir); renameErr != nil {
		if backupExists {
			_ = os.Rename(backup, plugin.dir)
		}

		return nil, nil, fmt.Errorf("installing staged plugin directory: %w", renameErr)
	}

	rollback := func() error {
		removeErr := os.RemoveAll(plugin.dir)
		if removeErr != nil {
			return fmt.Errorf("removing staged plugin directory: %w", removeErr)
		}
		if backupExists {
			if restoreErr := os.Rename(backup, plugin.dir); restoreErr != nil {
				return fmt.Errorf("restoring plugin directory backup: %w", restoreErr)
			}
		}

		return nil
	}
	commit := func() error {
		if !backupExists {
			return syncDir(parent)
		}
		if removeErr := os.RemoveAll(backup); removeErr != nil {
			return fmt.Errorf("removing plugin directory backup: %w", removeErr)
		}

		return syncDir(parent)
	}

	return rollback, commit, nil
}

func (plugin pluginDirectoryInstall) snippet() string {
	order := plugin.snippetOrder
	if len(order) == 0 {
		order = make([]string, 0, len(plugin.files))
		for name := range plugin.files {
			order = append(order, name)
		}
	}

	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, "== "+name+" ==\n"+plugin.files[name])
	}

	return strings.Join(parts, "\n")
}
