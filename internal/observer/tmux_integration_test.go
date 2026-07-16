package observer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

//nolint:cyclop,gocognit // end-to-end setup and assertions intentionally cover all four agents in one server
func TestRealTmuxBottomScreenDetectionForFourAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	name := fmt.Sprintf("agent-sessions-detect-%d", os.Getpid())
	ctx := context.Background()
	defer func() { _ = exec.CommandContext(context.Background(), "tmux", "-L", name, "kill-server").Run() }()

	tests := []struct {
		harness registry.Harness
		screen  string
		want    registry.Activity
	}{
		{registry.HarnessCodex, "Would you like to run the following command?", registry.ActivityWaiting},
		{registry.HarnessClaude, "Thinking… esc to interrupt", registry.ActivityRunning},
		{registry.HarnessOpenCode, "Ask anything", registry.ActivityIdle},
		{registry.HarnessPi, "Type a message · Enter to send", registry.ActivityIdle},
	}
	processes := make([]processinfo.Process, 0, len(tests))
	panes := make([]tmuxctx.Pane, 0, len(tests))
	for index, test := range tests {
		sessionName := string(test.harness)
		script := filepath.Join(t.TempDir(), sessionName+".sh")
		contents := "#!/bin/sh\nprintf '\\033[999;1H%s' " + shellSingleQuote(test.screen) + "\nexec sleep 60\n"
		if err := os.WriteFile(script, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(script, 0o700); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.CommandContext(ctx, "tmux", "-L", name, "-f", "/dev/null", "new-session", "-d", "-s", sessionName, script).CombinedOutput(); err != nil {
			t.Fatalf("start tmux session %s: %v: %s", sessionName, err, output)
		}
		output, err := exec.CommandContext(ctx, "tmux", "-L", name, "display-message", "-p", "-t", sessionName, "-F", "#{pane_id}|#{pane_tty}|#{pane_pid}").Output()
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Split(strings.TrimSpace(string(output)), "|")
		if len(fields) != 3 {
			t.Fatalf("tmux pane fields = %#v", fields)
		}
		panePID, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatal(err)
		}
		processPID := 5000 + index
		processes = append(processes, processinfo.Process{PID: processPID, PPID: panePID, ProcessGroupID: processPID, Foreground: true, StartIdentity: "test:" + sessionName, Executable: "/usr/bin/" + sessionName, CWD: "/tmp", TTY: fields[1], Args: []string{sessionName}})
		tmux := registry.TmuxContext{Inside: true, ServerSocket: "-L:" + name, SessionID: "$" + strconv.Itoa(index+1), SessionName: sessionName, WindowID: "@" + strconv.Itoa(index+1), WindowIndex: "0", WindowName: sessionName, PaneID: fields[0], PaneIndex: "0", PaneCurrentPath: "/tmp", PanePID: panePID, PaneTTY: fields[1]}
		pane := tmuxctx.Pane{Tmux: tmux, ServerIdentity: "-L:" + name, PanePID: panePID, PaneTTY: fields[1]}
		panes = append(panes, pane)
		deadline := time.Now().Add(2 * time.Second)
		for {
			snapshot, captureErr := tmuxctx.CapturePane(ctx, pane)
			if captureErr == nil && strings.Contains(snapshot.Text, test.screen) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("tmux pane %s did not render fixture %q: snapshot=%#v error=%v", fields[0], test.screen, snapshot, captureErr)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	store := registry.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	observer := New(Options{Store: store, ProcessList: func(context.Context) ([]processinfo.Process, error) { return processes, nil }, PaneList: func(context.Context) ([]tmuxctx.Pane, error) { return panes, nil }, CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil }, DetectionConfigDir: t.TempDir(), Now: func() time.Time { return time.Now().UTC() }})
	result, err := observer.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Degraded {
		t.Fatalf("real tmux observer degraded: %#v", result)
	}
	sessions, err := store.List(ctx, registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != len(tests) {
		t.Fatalf("sessions = %d, want %d: %#v", len(sessions), len(tests), sessions)
	}
	wantByHarness := make(map[registry.Harness]registry.Activity, len(tests))
	for _, test := range tests {
		wantByHarness[test.harness] = test.want
	}
	for _, session := range sessions {
		want := wantByHarness[session.Harness]
		if session.Activity == nil || *session.Activity != want || session.ActivityDecision == nil || session.ActivityDecision.Authority != "screen" {
			t.Errorf("session %s activity=%s screen=%#v, want %s screen activity", session.Harness, activityValue(session.Activity), *session.Observations.Screen, want)
		}
	}

	raceOptions := Options{Store: store, ProcessList: func(context.Context) ([]processinfo.Process, error) { return processes, nil }, PaneList: func(context.Context) ([]tmuxctx.Pane, error) { return panes, nil }, CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil }, DetectionConfigDir: t.TempDir(), Now: func() time.Time { return time.Now().UTC() }}
	raceOptions.ScreenCapture = func(captureCtx context.Context, pane tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error) {
		snapshot, captureErr := tmuxctx.CapturePane(captureCtx, pane)
		if captureErr != nil {
			return tmuxctx.ScreenSnapshot{}, fmt.Errorf("capture race fixture: %w", captureErr)
		}
		harnessID := registry.Harness(pane.Tmux.SessionName)
		if harnessID != registry.HarnessPi && harnessID != registry.HarnessOpenCode {
			return snapshot, nil
		}
		integration := "pi-extension"
		if harnessID == registry.HarnessOpenCode {
			integration = "opencode-plugin"
		}
		for _, process := range processes {
			if process.TTY != pane.PaneTTY {
				continue
			}
			running := registry.ActivityRunning
			presence := registry.PresenceLive
			if _, err := store.Observe(captureCtx, registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: harnessID, Identity: registry.ObservationIdentity{SessionID: "race-" + string(harnessID)}, Presence: &presence, Activity: &running, NativeEvent: "integration_race", Process: processIdentity(process), Attributes: map[string]string{"agent_sessions_integration": integration}, ObservedAt: time.Now().UTC()}); err != nil {
				return tmuxctx.ScreenSnapshot{}, fmt.Errorf("record integration race: %w", err)
			}
			break
		}
		return snapshot, nil
	}
	if raceResult, err := New(raceOptions).RunOnce(ctx); err != nil || raceResult.Degraded {
		t.Fatalf("real tmux race reconciliation = %#v, %v", raceResult, err)
	}
	sessions, err = store.List(ctx, registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.Harness != registry.HarnessPi && session.Harness != registry.HarnessOpenCode {
			continue
		}
		if session.Activity == nil || *session.Activity != registry.ActivityRunning || session.ActivityDecision == nil || session.ActivityDecision.Authority != "hook" {
			t.Errorf("real tmux race allowed fallback to overwrite %s integration: %#v", session.Harness, session)
		}
	}
}

func activityValue(value *registry.Activity) registry.Activity {
	if value == nil {
		return ""
	}
	return *value
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
