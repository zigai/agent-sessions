package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

var errRecursiveShimTarget = errors.New("target binary resolves to managed shim")

func installShim(options Options, harness registry.Harness) (Result, error) {
	dir := filepath.Join(registry.DefaultStateDir(), "shims")
	path := filepath.Join(dir, string(harness))
	target, err := resolveShimTarget(options.TargetBinary, string(harness), dir, path)
	if err != nil {
		return Result{}, err
	}
	script := shimScript(options.Binary, string(harness), target)

	changed, err := fileNeedsUpdate(path, script, options.Force)
	if err != nil {
		return Result{}, err
	}

	if changed && !options.DryRun {
		mkdirErr := os.MkdirAll(dir, 0o700)
		if mkdirErr != nil {
			return Result{}, fmt.Errorf("creating shim directory: %w", mkdirErr)
		}

		writeErr := os.WriteFile(path, []byte(script), 0o600)
		if writeErr != nil {
			return Result{}, fmt.Errorf("writing shim: %w", writeErr)
		}

		chmodErr := os.Chmod(path, 0o700)
		if chmodErr != nil {
			return Result{}, fmt.Errorf("making shim executable: %w", chmodErr)
		}
	}

	message := fmt.Sprintf("%s shim installed; put %s before the real harness binary in PATH", harness, dir)
	if !changed {
		message = fmt.Sprintf("%s shim already installed", harness)
	}

	if options.DryRun {
		message = fmt.Sprintf("dry run: %s shim not written", harness)
	}

	return Result{
		Harness: string(harness),
		Path:    path,
		Changed: changed,
		Message: message,
		Snippet: script,
		Error:   "",
	}, nil
}

func resolveShimTarget(target string, harness string, shimDir string, shimPath string) (string, error) {
	if target != "" {
		if pathInDir(target, shimDir) || samePath(target, shimPath) {
			return "", fmt.Errorf("%w: %s", errRecursiveShimTarget, target)
		}

		return target, nil
	}

	found, err := lookPathExcludingShimDir(harness, shimDir)
	if err != nil {
		return "", fmt.Errorf("finding %s binary: %w", harness, err)
	}
	if pathInDir(found, shimDir) || samePath(found, shimPath) {
		return "", fmt.Errorf("%w: %s", errRecursiveShimTarget, found)
	}

	return found, nil
}

func lookPathExcludingShimDir(file string, shimDir string) (string, error) {
	if strings.ContainsAny(file, `/\`) {
		if isExecutable(file) {
			return file, nil
		}

		return "", os.ErrNotExist
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.TrimSpace(dir) == "" || samePath(dir, shimDir) {
			continue
		}
		for _, candidate := range executableCandidates(filepath.Join(dir, file)) {
			if isExecutable(candidate) {
				return candidate, nil
			}
		}
	}

	return "", os.ErrNotExist
}

func executableCandidates(path string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(path) != "" {
		return []string{path}
	}

	extensions := filepath.SplitList(os.Getenv("PATHEXT"))
	if len(extensions) == 0 {
		extensions = []string{".COM", ".EXE", ".BAT", ".CMD"}
	}

	candidates := make([]string, 0, len(extensions)+1)
	candidates = append(candidates, path)
	for _, extension := range extensions {
		if extension == "" {
			continue
		}
		candidates = append(candidates, path+extension)
	}

	return candidates
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}

	return info.Mode()&0o111 != 0
}

func pathInDir(path string, dir string) bool {
	path = canonicalPath(path)
	dir = canonicalPath(dir)
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func samePath(left string, right string) bool {
	left = canonicalPath(left)
	right = canonicalPath(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}

	return left == right
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = evaluated
	}

	return filepath.Clean(path)
}

func shimScript(binary string, harness string, target string) string {
	return fmt.Sprintf(`#!/bin/sh
set -u

agent_sessions_managed_marker=%s
AGENT_SESSIONS_INTEGRATION_ID=%s-shim
AGENT_SESSIONS_INTEGRATION_VERSION=1
agent_sessions_bin=%s
harness_bin=%s

"$agent_sessions_bin" report --harness %s --state idle --event process.start --source %s-shim >/dev/null 2>&1 || true
"$harness_bin" "$@"
status=$?
"$agent_sessions_bin" report --harness %s --state exited --event process.exit --source %s-shim >/dev/null 2>&1 || true
exit "$status"
`, shellQuote(managedMarker), harness, shellQuote(binary), shellQuote(target), shellQuote(harness), shellQuote(harness), shellQuote(harness), shellQuote(harness))
}
