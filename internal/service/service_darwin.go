//go:build darwin

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	darwinLabel     = "dev.zigai.agent-sessions.observer"
	darwinPlistName = "dev.zigai.agent-sessions.observer.plist"
)

type darwinBackend struct {
	options  Options
	path     string
	rendered string
	domain   string
}

func platformBackend(options Options) (backend, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	rendered, err := RenderLaunchAgent(normalized)
	if err != nil {
		return nil, err
	}
	path, err := launchAgentPath()
	if err != nil {
		return nil, err
	}
	return &darwinBackend{options: normalized, path: path, rendered: rendered, domain: fmt.Sprintf("gui/%d", os.Getuid())}, nil
}

// RenderLaunchAgent returns the exact managed LaunchAgent plist for options.
func RenderLaunchAgent(options Options) (string, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return "", err
	}
	args := []string{normalized.Binary, "--store", normalized.StorePath, "observe", "--interval", normalized.Interval.String(), "--grace-period", normalized.GracePeriod.String(), "--quiet"}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!-- " + managedMarker + " -->\n<!-- version: 2 -->\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\"><dict>\n")
	b.WriteString("<key>Label</key><string>" + xmlEscape(darwinLabel) + "</string>\n")
	b.WriteString("<key>ProgramArguments</key><array>\n")
	for _, arg := range args {
		b.WriteString("<string>" + xmlEscape(arg) + "</string>\n")
	}
	b.WriteString("</array>\n<key>RunAtLoad</key><true/>\n")
	b.WriteString("<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n")
	b.WriteString("</dict></plist>\n")
	return b.String(), nil
}

// LaunchAgentPath returns the managed LaunchAgent plist path.
func LaunchAgentPath() (string, error) { return launchAgentPath() }

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", darwinPlistName), nil
}

func (b *darwinBackend) describe() Result {
	return Result{Platform: "darwin", Manager: "launchctl", ManagedPath: b.path, ManagedVersion: managedVersion, Path: b.path, Version: managedVersion}
}
func (b *darwinBackend) content() string                               { return b.rendered }
func (b *darwinBackend) reload(context.Context, CommandExecutor) error { return nil }
func (b *darwinBackend) load(ctx context.Context, executor CommandExecutor) error {
	if output, err := executor.Run(ctx, "launchctl", "bootstrap", b.domain, b.path); err != nil {
		return wrapManagerError("launchctl bootstrap observer", output, err)
	}
	return nil
}

func (b *darwinBackend) restart(ctx context.Context, executor CommandExecutor) error {
	if err := b.unload(ctx, executor); err != nil {
		return err
	}
	return b.load(ctx, executor)
}

func (b *darwinBackend) unload(ctx context.Context, executor CommandExecutor) error {
	output, err := executor.Run(ctx, "launchctl", "bootout", b.domain, b.path)
	if err != nil && !managerMissing(output) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return wrapManagerError("launchctl bootout observer", output, err)
	}
	return nil
}

func (b *darwinBackend) running(ctx context.Context, executor CommandExecutor) (bool, string) {
	_, err := executor.Run(ctx, "launchctl", "print", b.domain+"/"+darwinLabel)
	if err == nil {
		return true, "running"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err.Error()
	}
	return false, "not running"
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}

var _ backend = (*darwinBackend)(nil)
