package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zigai/agent-sessions/pkg/registry"
)

func installShim(options Options, harness registry.Harness) (Result, error) {
	target := options.TargetBinary
	if target == "" {
		found, err := exec.LookPath(string(harness))
		if err != nil {
			return Result{}, fmt.Errorf("finding %s binary: %w", harness, err)
		}

		target = found
	}

	dir := filepath.Join(registry.DefaultStateDir(), "shims")
	path := filepath.Join(dir, string(harness))
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
