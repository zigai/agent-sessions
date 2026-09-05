//go:build linux

package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeOptionsResolvesBinaryFromPath(t *testing.T) {
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "aht")
	//nolint:gosec // This private fixture must be executable for executable-path lookup.
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	options, err := normalizeOptions(Options{Binary: "aht", StorePath: "state.json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Binary != binary {
		t.Fatalf("resolved executable = %q, want %q", options.Binary, binary)
	}
	_, err = normalizeOptions(Options{Binary: "missing-aht", StorePath: "state.json"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing executable error = %v", err)
	}
}

func TestStatusPropagatesManagerFailures(t *testing.T) {
	t.Parallel()
	backend := linuxBackend{}
	for _, cause := range []error{exec.ErrNotFound, os.ErrPermission, errManagerTestFailure, context.Canceled} {
		running, _, err := backend.running(t.Context(), &recordingExecutor{err: cause})
		if running || !errors.Is(err, cause) {
			t.Errorf("status for %v = (%v, %v), want original failure", cause, running, err)
		}
	}
}

func TestExecutorPreservesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (osCommandExecutor{}).Run(ctx, "sh", "-c", "exit 0")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("executor cancellation error = %v", err)
	}
}
