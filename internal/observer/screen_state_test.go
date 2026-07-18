package observer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

func TestObserverDetectsScreenStateForFourTargetAgents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		harness registry.Harness
		command string
		screen  string
		want    registry.Activity
	}{
		{registry.HarnessCodex, "codex", "› next task\nContext 63% used", registry.ActivityIdle},
		{registry.HarnessClaude, "claude", "Do you want to proceed?", registry.ActivityWaiting},
		{registry.HarnessOpenCode, "opencode", "Working · esc to interrupt", registry.ActivityRunning},
		{registry.HarnessPi, "pi", "Type a message · Enter to send", registry.ActivityIdle},
	}
	for index, test := range tests {
		t.Run(string(test.harness), func(t *testing.T) {
			t.Parallel()
			store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			process, pane := detectionProcessPane(index+100, test.command)
			options := detectionObserverOptions(store, process, pane, t.TempDir())
			options.ScreenCapture = func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
				return tmuxctx.ScreenSnapshot{Text: test.screen, Title: test.command}, nil
			}
			if _, err := New(options).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			sessions, err := store.List(context.Background(), registry.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != test.want || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Authority != "screen" {
				t.Fatalf("sessions = %#v, want one %s screen decision", sessions, test.want)
			}
			if (test.harness == registry.HarnessPi || test.harness == registry.HarnessOpenCode) && sessions[0].ActivityDecision.FallbackReason != "integration_report_missing" {
				t.Fatalf("screen fallback reason = %q, want integration_report_missing", sessions[0].ActivityDecision.FallbackReason)
			}
		})
	}
}

func TestObserverRecordsUnknownWhenStaleIntegrationHasNoTmuxPane(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	process, pane := detectionProcessPane(197, "pi")
	identity := observerProcessIdentity(process)
	presence := registry.PresenceLive
	idle := registry.ActivityIdle
	now := time.Now().UTC()
	_, err := store.Observe(context.Background(), registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: registry.HarnessPi, Identity: registry.ObservationIdentity{SessionID: "stale-pi"}, Presence: &presence, Activity: &idle, NativeEvent: "agent_settled", Process: identity, Attributes: map[string]string{"agent_sessions_integration": "pi-extension"}, ObservedAt: now.Add(-registry.IntegrationActivityLease - time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	options := detectionObserverOptions(store, process, pane, t.TempDir())
	options.PaneList = func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil }
	captureCalled := false
	options.ScreenCapture = func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
		captureCalled = true
		return tmuxctx.ScreenSnapshot{}, nil
	}
	if _, err := New(options).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if captureCalled {
		t.Fatal("observer attempted tmux capture without a pane")
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityUnknown || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Reason != "screen_not_in_tmux" || sessions[0].ActivityDecision.FallbackReason != "integration_report_stale" {
		t.Fatalf("stale integration without pane state = %#v", sessions)
	}
}

func TestObserverRecordsUnknownWhenScreenCaptureFails(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	process, pane := detectionProcessPane(198, "codex")
	options := detectionObserverOptions(store, process, pane, t.TempDir())
	options.ScreenCapture = func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
		return tmuxctx.ScreenSnapshot{}, context.Canceled
	}
	observer := New(options)
	result, err := observer.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Degraded {
		t.Fatalf("capture failure did not degrade observer: %#v", result)
	}
	if health := observer.Health(); !health.Degraded || health.LastEnumerationErrorCategory != "reconciliation" || !strings.Contains(health.LastEnumerationError, "capturing codex pane") {
		t.Fatalf("capture failure health = %#v", health)
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityUnknown || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Reason != "screen_unavailable" {
		t.Fatalf("capture failure state = %#v", sessions)
	}
}

func TestObserverNeverPersistsRawTerminalContents(t *testing.T) {
	t.Parallel()
	const secret = "PRIVATE-COMMAND-ARGUMENT"
	path := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(path)
	process, pane := detectionProcessPane(199, "codex")
	options := detectionObserverOptions(store, process, pane, t.TempDir())
	options.ScreenCapture = func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
		return tmuxctx.ScreenSnapshot{Text: "Would you like to run the following command? " + secret, Title: secret}, nil
	}
	if _, err := New(options).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "Would you like to run") {
		t.Fatalf("registry persisted terminal contents: %s", data)
	}
}

func TestObserverScreenDetectionRecoversMissedPermissionTransition(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	process, pane := detectionProcessPane(201, "codex")
	at := time.Now().UTC()
	screen := "Would you like to run the following command?"
	options := detectionObserverOptions(store, process, pane, t.TempDir())
	options.Now = func() time.Time { return at }
	options.ScreenCapture = func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
		return tmuxctx.ScreenSnapshot{Text: screen, Title: "codex"}, nil
	}
	observer := New(options)
	if _, err := observer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	screen = "› continue\nContext 63% used"
	at = at.Add(time.Second)
	if _, err := observer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityIdle || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.RuleID != "input_prompt" {
		t.Fatalf("missed transition was not corrected: %#v", sessions)
	}
}

func TestScreenFallbackCannotOverrideConcurrentCompleteIntegration(t *testing.T) {
	t.Parallel()
	for _, harness := range []registry.Harness{registry.HarnessPi, registry.HarnessOpenCode} {
		t.Run(string(harness), func(t *testing.T) {
			t.Parallel()
			store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			process, pane := detectionProcessPane(300+len(harness), string(harness))
			options := detectionObserverOptions(store, process, pane, t.TempDir())
			options.ScreenCapture = func(ctx context.Context, _ tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
				running := registry.ActivityRunning
				presence := registry.PresenceLive
				integration := "pi-extension"
				if harness == registry.HarnessOpenCode {
					integration = "opencode-plugin"
				}
				identity := observerProcessIdentity(process)
				_, err := store.Observe(ctx, registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: harness, Identity: registry.ObservationIdentity{SessionID: "active"}, Presence: &presence, Activity: &running, NativeEvent: "agent_start", Process: identity, Attributes: map[string]string{"agent_sessions_integration": integration}, ObservedAt: time.Now().UTC()})
				if err != nil {
					return tmuxctx.ScreenSnapshot{}, fmt.Errorf("record concurrent integration report: %w", err)
				}
				return tmuxctx.ScreenSnapshot{Text: "Type a message · Enter to send"}, nil
			}
			if _, err := New(options).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			sessions, err := store.List(context.Background(), registry.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityRunning || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Authority != "hook" {
				t.Fatalf("screen fallback overrode active integration: %#v", sessions)
			}
		})
	}
}

func detectionObserverOptions(store registry.Store, process processinfo.Process, pane tmuxctx.Pane, configDir string) Options {
	return Options{
		Store:              store,
		ProcessList:        func(context.Context) ([]processinfo.Process, error) { return []processinfo.Process{process}, nil },
		PaneList:           func(context.Context) ([]tmuxctx.Pane, error) { return []tmuxctx.Pane{pane}, nil },
		CatalogList:        func(context.Context) ([]CatalogEntry, error) { return nil, nil },
		DetectionConfigDir: configDir,
		HealthPath:         filepath.Join("", ""),
		Now:                func() time.Time { return time.Now().UTC() },
	}
}

func detectionProcessPane(pid int, command string) (processinfo.Process, tmuxctx.Pane) {
	process := processinfo.Process{PID: pid, PPID: 1, ProcessGroupID: pid, Foreground: true, StartIdentity: "boot:" + command, Executable: "/usr/bin/" + command, CWD: "/work", TTY: "/dev/pts/9", Args: []string{command}}
	tmux := registry.TmuxContext{Inside: true, ServerSocket: "default", SessionID: "$1", SessionName: "agents", WindowID: "@1", WindowIndex: "1", WindowName: "agents", PaneID: "%9", PaneIndex: "1", PaneCurrentPath: "/work", PanePID: 10, PaneTTY: "/dev/pts/9"}
	return process, tmuxctx.Pane{Tmux: tmux, ServerIdentity: "default", PanePID: 10, PaneTTY: "/dev/pts/9"}
}

func observerProcessIdentity(process processinfo.Process) *registry.ProcessIdentity {
	return &registry.ProcessIdentity{PID: process.PID, PPID: process.PPID, ProcessGroupID: process.ProcessGroupID, Foreground: process.Foreground, StartIdentity: process.StartIdentity, Executable: process.Executable, CWD: process.CWD, TTY: process.TTY}
}
