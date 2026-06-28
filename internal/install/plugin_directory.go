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
	info, err := os.Stat(plugin.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking plugin directory: %w", err)
	}
	if !info.IsDir() {
		return false, nil
	}
	if plugin.markerFile == "" {
		return false, nil
	}

	marker, err := os.ReadFile(filepath.Join(plugin.dir, plugin.markerFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("reading plugin marker: %w", err)
	}

	return strings.Contains(string(marker), managedMarker), nil
}

func (plugin pluginDirectoryInstall) needsUpdate() (bool, error) {
	info, err := os.Stat(plugin.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("checking plugin directory %s: %w", plugin.dir, err)
	}
	if !info.IsDir() {
		return true, nil
	}

	for name, content := range plugin.files {
		path := filepath.Join(plugin.dir, name)
		current, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}

			return false, fmt.Errorf("reading plugin file %s: %w", path, err)
		}
		if string(current) != content {
			return true, nil
		}
	}

	entries, err := os.ReadDir(plugin.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("reading plugin directory %s: %w", plugin.dir, err)
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
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("creating plugin parent directory: %w", err)
	}

	stagedDir, err := os.MkdirTemp(parent, "."+filepath.Base(plugin.dir)+".stage-*")
	if err != nil {
		return "", fmt.Errorf("creating staged plugin directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stagedDir)
		}
	}()

	for name, content := range plugin.files {
		path := filepath.Join(stagedDir, name)
		if err := writeStagedPluginFile(path, []byte(content)); err != nil {
			return "", fmt.Errorf("writing plugin file %s: %w", path, err)
		}
	}
	if err := syncDir(stagedDir); err != nil {
		return "", err
	}
	keep = true

	return stagedDir, nil
}

func writeStagedPluginFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("opening staged plugin file: %w", err)
	}
	keepOpen := true
	defer func() {
		if keepOpen {
			_ = file.Close()
		}
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("writing staged plugin file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing staged plugin file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing staged plugin file: %w", err)
	}
	keepOpen = false

	return nil
}

func (plugin pluginDirectoryInstall) installStaged() (func() error, func() error, error) {
	stagedDir, err := plugin.stage()
	if err != nil {
		return nil, nil, err
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
	backup, err := os.MkdirTemp(parent, "."+filepath.Base(plugin.dir)+".backup-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating plugin backup path: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return nil, nil, fmt.Errorf("preparing plugin backup path: %w", err)
	}

	backupExists := false
	if err := os.Rename(plugin.dir, backup); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("backing up plugin directory: %w", err)
		}
	} else {
		backupExists = true
	}

	if err := os.Rename(stagedDir, plugin.dir); err != nil {
		if backupExists {
			_ = os.Rename(backup, plugin.dir)
		}

		return nil, nil, fmt.Errorf("installing staged plugin directory: %w", err)
	}

	rollback := func() error {
		err := os.RemoveAll(plugin.dir)
		if err != nil {
			return fmt.Errorf("removing staged plugin directory: %w", err)
		}
		if backupExists {
			if err := os.Rename(backup, plugin.dir); err != nil {
				return fmt.Errorf("restoring plugin directory backup: %w", err)
			}
		}

		return nil
	}
	commit := func() error {
		if !backupExists {
			return syncDir(parent)
		}
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("removing plugin directory backup: %w", err)
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
