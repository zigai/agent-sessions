package observer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

var errFailGoneObservation = errors.New("fail gone observation once")

type failGoneOnceStore struct {
	registry.Store

	failed bool
}

func (store *failGoneOnceStore) ObserveBatch(ctx context.Context, observations []registry.Observation) ([]registry.Session, error) {
	if !store.failed {
		for _, observation := range observations {
			if observation.ProcessPresent != nil && !*observation.ProcessPresent {
				store.failed = true
				return nil, errFailGoneObservation
			}
		}
	}

	sessions, err := store.Store.ObserveBatch(ctx, observations)
	if err != nil {
		return nil, fmt.Errorf("delegate gone-observation store: %w", err)
	}

	return sessions, nil
}

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

func TestObserverRetriesFailedGoneObservationAndEvictsTrackedProcess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sessions.json")
	baseStore := registry.NewFileStore(path)
	store := &failGoneOnceStore{Store: baseStore}
	at := time.Now().UTC().Add(-time.Minute)
	process := processinfo.Process{PID: 1234, PPID: 1, ProcessGroupID: 1234, StartIdentity: "boot:A", Executable: "/usr/bin/codex", CWD: "/work", TTY: "/dev/pts/1"}
	processes := []processinfo.Process{process}
	watcher := New(Options{
		Store: store,
		Now:   func() time.Time { return at },
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			return processes, nil
		},
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
	})
	if _, err := watcher.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	processes = nil
	at = at.Add(time.Second)
	if _, err := watcher.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	failed, err := watcher.RunOnce(context.Background())
	if !errors.Is(err, errFailGoneObservation) || failed.Gone != 1 {
		t.Fatalf("failed gone cycle = %#v, err=%v", failed, err)
	}
	if len(watcher.tracked) != 1 {
		t.Fatalf("failed gone cycle retired tracked process: %#v", watcher.tracked)
	}
	at = at.Add(time.Second)
	retried, err := watcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if retried.Gone != 1 || len(watcher.tracked) != 0 {
		t.Fatalf("successful retry = %#v, tracked=%#v", retried, watcher.tracked)
	}
	sessions, err := baseStore.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Presence != registry.PresenceGone {
		t.Fatalf("sessions after retry = %#v", sessions)
	}
}

func TestObserverRejectsConcurrentRunsOnOneInstance(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	watcher := New(Options{
		Store: registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json")),
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			close(entered)
			<-release
			return nil, nil
		},
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
	})
	firstDone := make(chan error, 1)
	go func() {
		_, err := watcher.RunOnce(context.Background())
		firstDone <- err
	}()
	<-entered
	if _, err := watcher.RunOnce(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("concurrent RunOnce() error = %v, want ErrAlreadyRunning", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
}

func TestObserverRetriesHealthWriteAfterPersistenceFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blockedParent := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	healthPath := filepath.Join(blockedParent, "health.json")
	at := time.Now().UTC()
	watcher := New(Options{
		Store:       registry.NewFileStore(filepath.Join(root, "sessions.json")),
		HealthPath:  healthPath,
		Now:         func() time.Time { return at },
		ProcessList: func(context.Context) ([]processinfo.Process, error) { return nil, nil },
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
	})
	if _, err := watcher.RunOnce(context.Background()); err == nil {
		t.Fatal("expected health persistence failure")
	}
	if err := os.Remove(blockedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blockedParent, 0o700); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	if _, err := watcher.RunOnce(context.Background()); err != nil {
		t.Fatalf("health write retry error = %v", err)
	}
	if _, err := os.Stat(healthPath); err != nil {
		t.Fatalf("health file was not retried: %v", err)
	}
}

func TestObserverRestartMarksMissingStoredProcessGone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.json")
	at := time.Now().UTC().Add(-time.Minute)
	process := processinfo.Process{PID: 1234, PPID: 1, ProcessGroupID: 1234, StartIdentity: "boot:A", Executable: "/usr/bin/codex", CWD: "/work", TTY: "/dev/pts/1"}
	processes := []processinfo.Process{process}
	options := Options{
		StorePath: path,
		Now:       func() time.Time { return at },
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			return processes, nil
		},
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
		HealthPath:  path + ".health",
	}
	if _, err := New(options).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	processes = nil
	at = at.Add(time.Second)
	result, err := New(options).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Gone != 1 {
		t.Fatalf("restart result gone = %d, want 1: %#v", result.Gone, result)
	}

	sessions, err := registry.NewFileStore(path).List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Presence != registry.PresenceGone || sessions[0].Activity != nil || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Reason != "process_gone" || sessions[0].ActivityDecision.Process.StartIdentity != process.StartIdentity {
		t.Fatalf("sessions after restart: %#v", sessions)
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
