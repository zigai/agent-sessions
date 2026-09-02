//go:build linux && integration

package observer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/aht/internal/herdrctx"
	"github.com/zigai/aht/internal/muxctx"
	"github.com/zigai/aht/pkg/registry"
	"github.com/zigai/aht/internal/zellijctx"
)

func TestCurrentZellijDiscoveryAndCapture(t *testing.T) {
	zellij, err := exec.LookPath("zellij")
	if err != nil {
		t.Skip("Zellij is not installed")
	}
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is required to allocate Zellij's controlling terminal")
	}

	session := fmt.Sprintf("aht-zellij-%d", time.Now().UnixNano())
	configDir := t.TempDir()
	t.Setenv("ZELLIJ_CONFIG_DIR", configDir)
	logPath := filepath.Join(t.TempDir(), "zellij.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create Zellij log: %v", err)
	}
	commandLine := strings.Join([]string{
		shellSingleQuote(zellij), "--config-dir", shellSingleQuote(configDir),
		"--session", shellSingleQuote(session),
	}, " ")
	process := exec.Command(script, "-q", "-c", commandLine, "/dev/null")
	process.Stdout = logFile
	process.Stderr = logFile
	if err := process.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start Zellij session: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, zellij, "--config-dir", configDir, "kill-session", session).Run()
		if process.Process != nil {
			_ = process.Process.Kill()
		}
		_ = process.Wait()
		_ = logFile.Close()
	})

	waitForCommandSuccess(t, 10*time.Second, zellij, "--config-dir", configDir, "--session", session, "action", "list-panes", "--all", "--json")
	marker := "AHT_ZELLIJ_CURRENT_VERSION_CAPTURE"
	run := exec.Command(zellij, "--config-dir", configDir, "--session", session, "run", "--", "sh", "-c", "printf '%s\\n' "+shellSingleQuote(marker)+"; sleep 30")
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("create Zellij test pane: %v\n%s\n%s", err, output, readProbeLog(logPath))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pane := waitForMultiplexerPane(t, ctx, registry.MultiplexerZellij, session, strings.TrimSpace(string(output)), func(ctx context.Context) ([]muxctx.Pane, error) {
		return zellijctx.ListPanes(ctx)
	})
	waitForPaneCapture(t, ctx, pane, marker, zellijctx.CapturePane)
}

func TestCurrentHerdrDiscoveryAndCapture(t *testing.T) {
	herdr, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("Herdr is not installed")
	}

	session := fmt.Sprintf("aht-herdr-%d", time.Now().UnixNano())
	home, err := os.MkdirTemp("/tmp", "aht-herdr-home-")
	if err != nil {
		t.Fatalf("create short Herdr home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("HERDR_SESSION", session)
	logPath := filepath.Join(t.TempDir(), "herdr.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create Herdr log: %v", err)
	}
	process := exec.Command(herdr, "server")
	process.Stdout = logFile
	process.Stderr = logFile
	if err := process.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start Herdr server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stop := exec.CommandContext(ctx, herdr, "session", "stop", session, "--json")
		stop.Env = os.Environ()
		_ = stop.Run()
		if process.Process != nil {
			_ = process.Process.Kill()
		}
		_ = process.Wait()
		_ = logFile.Close()
	})

	waitForCommandSuccess(t, 10*time.Second, herdr, "api", "snapshot")
	workspace := exec.Command(herdr, "workspace", "create", "--cwd", t.TempDir(), "--label", "aht-test", "--no-focus")
	workspace.Env = os.Environ()
	output, err := workspace.CombinedOutput()
	if err != nil {
		t.Fatalf("create Herdr workspace: %v\n%s\n%s", err, output, readProbeLog(logPath))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pane := waitForMultiplexerPane(t, ctx, registry.MultiplexerHerdr, session, "", func(ctx context.Context) ([]muxctx.Pane, error) {
		return herdrctx.ListPanes(ctx)
	})
	marker := "AHT_HERDR_CURRENT_VERSION_CAPTURE"
	run := exec.Command(herdr, "pane", "run", pane.Location.PaneID, "printf '%s\\n' "+shellSingleQuote(marker))
	run.Env = os.Environ()
	output, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("write Herdr test pane: %v\n%s\n%s", err, output, readProbeLog(logPath))
	}
	waitForPaneCapture(t, ctx, pane, marker, herdrctx.CapturePane)
}

func waitForCommandSuccess(t *testing.T, timeout time.Duration, name string, args ...string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var output []byte
	var err error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		command := exec.CommandContext(ctx, name, args...)
		command.Env = os.Environ()
		output, err = command.CombinedOutput()
		cancel()
		if err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("command %s %s did not become ready: %v\n%s", name, strings.Join(args, " "), err, output)
}

func waitForMultiplexerPane(
	t *testing.T,
	ctx context.Context,
	kind registry.MultiplexerKind,
	session string,
	paneID string,
	list func(context.Context) ([]muxctx.Pane, error),
) muxctx.Pane {
	t.Helper()
	var lastErr error
	for ctx.Err() == nil {
		panes, err := list(ctx)
		if err != nil {
			lastErr = err
		} else {
			for _, pane := range panes {
				if pane.Location.Kind != kind || pane.Location.SessionName != session {
					continue
				}
				if paneID == "" || pane.Location.PaneID == paneID {
					return pane
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s pane for session %q was not discovered: %v", kind, session, lastErr)
	return muxctx.Pane{}
}

func waitForPaneCapture(
	t *testing.T,
	ctx context.Context,
	pane muxctx.Pane,
	marker string,
	capture func(context.Context, muxctx.Pane) (muxctx.ScreenSnapshot, error),
) {
	t.Helper()
	var last muxctx.ScreenSnapshot
	var lastErr error
	for ctx.Err() == nil {
		last, lastErr = capture(ctx, pane)
		if lastErr == nil && strings.Contains(last.Text, marker) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pane %s capture did not contain %q: %v\n%s", pane.Location.PaneID, marker, lastErr, last.Text)
}

func readProbeLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
