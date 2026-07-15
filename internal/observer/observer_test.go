package observer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/internal/processinfo"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

//nolint:cyclop // lifecycle test covers the two-snapshot disappearance contract
func TestObserverDefaultMissingRequiresTwoSnapshots(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.json")
	at := time.Now().UTC().Add(-time.Minute)
	process := processinfo.Process{PID: 1234, PPID: 1, ProcessGroupID: 1234, StartIdentity: "boot:A", Executable: "/usr/bin/codex", CWD: "/work", TTY: "/dev/pts/1"}
	processes := []processinfo.Process{process}
	watcher := New(Options{StorePath: path, Now: func() time.Time { return at }, ProcessList: func(context.Context) ([]processinfo.Process, error) { return processes, nil }, PaneList: func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil }, HealthPath: path + ".health"})
	first, err := watcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Present != 1 || first.Gone != 0 {
		t.Fatalf("first result: %#v", first)
	}
	sessions, err := registry.NewFileStore(path).List(context.Background(), registry.Filter{})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("present sessions: %v %#v", err, sessions)
	}
	session := sessions[0]
	if session.Presence != registry.PresenceLive || session.Activity == nil || *session.Activity != registry.ActivityUnknown {
		t.Fatalf("present session: %#v", session)
	}
	processes = nil
	at = at.Add(time.Second)
	second, err := watcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Gone != 0 {
		t.Fatalf("one miss marked gone: %#v", second)
	}
	at = at.Add(time.Second)
	third, err := watcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.Gone != 1 {
		t.Fatalf("second miss did not mark gone: %#v", third)
	}
	session, err = registry.NewFileStore(path).Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != registry.PresenceGone || session.Activity != nil {
		t.Fatalf("gone session: %#v", session)
	}
}

func TestResolveHarnessIgnoresLaterArguments(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{Executable: "/usr/bin/tmux", Args: []string{"/usr/bin/tmux", "new-session", "-s", "agent-test", "/tmp/codex"}}
	if harnessID, ok := resolveHarness(process); ok || harnessID != "" {
		t.Fatalf("tmux launcher was classified as harness: %q", harnessID)
	}
	process.Args = []string{"/tmp/codex", "resume"}
	if harnessID, ok := resolveHarness(process); !ok || harnessID != registry.HarnessCodex {
		t.Fatalf("codex argv was not classified: %q %t", harnessID, ok)
	}
}

func TestObserverCatalogCorrelatesCurrentClaudeProcess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.json")
	at := time.Now().UTC().Add(-time.Minute)
	process := processinfo.Process{PID: 42, PPID: 1, ProcessGroupID: 42, StartIdentity: "boot:A", Executable: "/usr/bin/claude"}
	watcher := New(Options{StorePath: path, Now: func() time.Time { return at }, ProcessList: func(context.Context) ([]processinfo.Process, error) { return []processinfo.Process{process}, nil }, PaneList: func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil }, CatalogList: func(context.Context) ([]CatalogEntry, error) {
		return []CatalogEntry{{Harness: registry.HarnessClaude, SessionID: "agent-1", ProcessPID: 42, Current: true}}, nil
	}})
	if _, err := watcher.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions, err := registry.NewFileStore(path).List(context.Background(), registry.Filter{})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("correlated sessions: %v %#v", err, sessions)
	}
	session := sessions[0]
	if session.Presence != registry.PresenceLive || session.SessionID != "agent-1" {
		t.Fatalf("correlated session: %#v", session)
	}
	if session.Observations.Catalog == nil || session.Observations.Process == nil {
		t.Fatalf("missing source evidence: %#v", session.Observations)
	}
}

func TestRunWithResultsStreamsEveryCycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	results := make([]Result, 0, 1)
	watcher := New(Options{
		Store: registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json")),
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			cancel()
			return nil, nil
		},
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
	})
	if err := watcher.RunWithResults(ctx, func(result Result) error {
		results = append(results, result)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
}
