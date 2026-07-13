//go:build linux

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const linuxUnitName = "agent-sessions-observer.service"

type linuxBackend struct {
	options  Options
	path     string
	rendered string
}

func platformBackend(options Options) (backend, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	rendered, err := RenderSystemdUnit(normalized)
	if err != nil {
		return nil, err
	}
	path, err := systemdUnitPath()
	if err != nil {
		return nil, err
	}
	return &linuxBackend{options: normalized, path: path, rendered: rendered}, nil
}

// RenderSystemdUnit returns the exact managed user unit for options.
func RenderSystemdUnit(options Options) (string, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		"# " + managedMarker,
		"# version: 2",
		"[Unit]",
		"Description=Agent Sessions observer",
		"",
		"[Service]",
		"ExecStart=" + systemdArg(normalized.Binary) + " --store " + systemdArg(normalized.StorePath) + " observe --interval " + normalized.Interval.String() + " --grace-period " + normalized.GracePeriod.String() + " --quiet",
		"Restart=on-failure",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n"), nil
}

// SystemdUnitPath returns the managed user unit path.
func SystemdUnitPath() (string, error) { return systemdUnitPath() }

func systemdUnitPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", linuxUnitName), nil
}

func (b *linuxBackend) describe() Result {
	return Result{Platform: "linux", Manager: "systemd", ManagedPath: b.path, ManagedVersion: managedVersion, Path: b.path, Version: managedVersion, Installed: false, Current: false, Running: false, Changed: false, Message: ""}
}
func (b *linuxBackend) content() string { return b.rendered }

func (b *linuxBackend) reload(ctx context.Context, executor CommandExecutor) error {
	if output, err := executor.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return wrapManagerError("systemctl daemon-reload", output, err)
	}
	return nil
}

func (b *linuxBackend) load(ctx context.Context, executor CommandExecutor) error {
	if output, err := executor.Run(ctx, "systemctl", "--user", "enable", "--now", linuxUnitName); err != nil {
		return wrapManagerError("systemctl enable observer", output, err)
	}
	return nil
}

func (b *linuxBackend) restart(ctx context.Context, executor CommandExecutor) error {
	if output, err := executor.Run(ctx, "systemctl", "--user", "restart", linuxUnitName); err != nil {
		return wrapManagerError("systemctl restart observer", output, err)
	}
	return nil
}

func (b *linuxBackend) unload(ctx context.Context, executor CommandExecutor) error {
	output, err := executor.Run(ctx, "systemctl", "--user", "disable", "--now", linuxUnitName)
	if err != nil && !managerMissing(output) {
		return wrapManagerError("systemctl disable observer", output, err)
	}
	return nil
}

func (b *linuxBackend) running(ctx context.Context, executor CommandExecutor) (bool, string) {
	_, err := executor.Run(ctx, "systemctl", "--user", "is-active", "--quiet", linuxUnitName)
	if err == nil {
		return true, "running"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err.Error()
	}
	return false, "not running"
}

func systemdArg(value string) string {
	if value == "" {
		return "\"\""
	}
	if strings.IndexFunc(value, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '"' || r == '\\' }) < 0 {
		return value
	}
	return strconv.Quote(value)
}

var _ backend = (*linuxBackend)(nil)
