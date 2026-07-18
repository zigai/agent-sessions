package observer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/muxctx"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestObserverCorrelatesZellijEnvironmentIdentityAndDetectsScreen(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	process := processinfo.Process{
		PID: 42, PPID: 1, ProcessGroupID: 42, Foreground: true, StartIdentity: "boot:42",
		Executable: "/usr/bin/codex", CWD: "/repo", MultiplexerKind: "zellij",
		MultiplexerSession: "work", MultiplexerPane: "7",
	}
	pane := muxctx.Pane{
		Location: registry.MultiplexerContext{
			Kind: registry.MultiplexerZellij, SessionName: "work", TabID: "3", TabName: "agents",
			PaneID: "terminal_7", PaneCurrentPath: "/repo",
		},
		Command: "codex", CWD: "/repo", Title: "Codex",
	}
	observer := New(Options{
		Store: store,
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			return []processinfo.Process{process}, nil
		},
		MultiplexerPaneList: func(context.Context) ([]muxctx.Pane, error) {
			return []muxctx.Pane{pane}, nil
		},
		MultiplexerScreenCapture: func(context.Context, muxctx.Pane) (muxctx.ScreenSnapshot, error) {
			return muxctx.ScreenSnapshot{Text: "› next task\nContext 63% used", Title: "Codex"}, nil
		},
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
		Now:         func() time.Time { return time.Now().UTC() },
	})
	if _, err := observer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Multiplexer.Kind != registry.MultiplexerZellij || sessions[0].Multiplexer.PaneID != "terminal_7" || !sessions[0].Tmux.Empty() {
		t.Fatalf("zellij session = %#v", sessions)
	}
	if sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityIdle || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Authority != "screen" {
		t.Fatalf("zellij activity = %#v", sessions[0])
	}
}

func TestObserverUsesHerdrForegroundProcessAndSemanticState(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	process := processinfo.Process{
		PID: 42, PPID: 40, ProcessGroupID: 42, StartIdentity: "boot:42",
		Executable: "/usr/bin/claude", CWD: "/repo",
	}
	waiting := registry.ActivityWaiting
	pane := muxctx.Pane{
		Location: registry.MultiplexerContext{
			Kind: registry.MultiplexerHerdr, SessionName: "work", WorkspaceID: "w1", TabID: "w1:t1",
			PaneID: "w1:p1", PaneCurrentPath: "/repo", PanePID: 42,
		},
		Processes: []muxctx.ProcessRef{{PID: 42, ProcessGroupID: 42, Command: "claude", CWD: "/repo"}},
		Activity:  &waiting, StateReason: "herdr_agent_status",
	}
	captureCalled := false
	observer := New(Options{
		Store: store,
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			return []processinfo.Process{process}, nil
		},
		MultiplexerPaneList: func(context.Context) ([]muxctx.Pane, error) {
			return []muxctx.Pane{pane}, nil
		},
		MultiplexerScreenCapture: func(context.Context, muxctx.Pane) (muxctx.ScreenSnapshot, error) {
			captureCalled = true
			return muxctx.ScreenSnapshot{}, nil
		},
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
		Now:         func() time.Time { return time.Now().UTC() },
	})
	if _, err := observer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if captureCalled {
		t.Fatal("observer captured the Herdr buffer despite semantic agent state")
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Multiplexer.Kind != registry.MultiplexerHerdr || sessions[0].Multiplexer.PaneID != "w1:p1" {
		t.Fatalf("herdr session = %#v", sessions)
	}
	if sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityWaiting || sessions[0].ActivityDecision == nil || sessions[0].ActivityDecision.Authority != "herdr" || sessions[0].ActivityDecision.Reason != "herdr_agent_status" {
		t.Fatalf("herdr activity = %#v", sessions[0])
	}
}

func TestObserverPrefersRicherNestedMultiplexerLocation(t *testing.T) {
	t.Parallel()
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	process := processinfo.Process{
		PID: 42, PPID: 40, ProcessGroupID: 42, StartIdentity: "boot:42", Executable: "/usr/bin/codex", CWD: "/repo",
		MultiplexerKind: "zellij", MultiplexerSession: "outer", MultiplexerPane: "7",
	}
	panes := []muxctx.Pane{
		{
			Location: registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "outer", PaneID: "terminal_7", PaneCurrentPath: "/repo"},
			Command:  "codex", CWD: "/repo",
		},
		{
			Location:  registry.MultiplexerContext{Kind: registry.MultiplexerHerdr, SessionName: "inner", WorkspaceID: "w1", TabID: "w1:t1", PaneID: "w1:p1", PaneCurrentPath: "/repo"},
			Processes: []muxctx.ProcessRef{{PID: 42, ProcessGroupID: 42, Command: "codex", CWD: "/repo"}},
		},
	}
	observer := New(Options{
		Store: store,
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			return []processinfo.Process{process}, nil
		},
		MultiplexerPaneList: func(context.Context) ([]muxctx.Pane, error) { return panes, nil },
		MultiplexerScreenCapture: func(context.Context, muxctx.Pane) (muxctx.ScreenSnapshot, error) {
			return muxctx.ScreenSnapshot{Text: "› next task"}, nil
		},
		CatalogList: func(context.Context) ([]CatalogEntry, error) { return nil, nil },
		Now:         func() time.Time { return time.Now().UTC() },
	})
	if _, err := observer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Multiplexer.Kind != registry.MultiplexerHerdr || sessions[0].Multiplexer.PaneID != "w1:p1" {
		t.Fatalf("nested location = %#v", sessions)
	}
}

func TestCommandCWDFallbackRequiresUniquePane(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{PID: 42, StartIdentity: "boot:42", Executable: "/usr/bin/codex", CWD: "/repo"}
	panes := []muxctx.Pane{
		{Location: registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "one", PaneID: "terminal_1"}, Command: "codex", CWD: "/repo"},
		{Location: registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "two", PaneID: "terminal_2"}, Command: "codex", CWD: "/repo"},
	}
	counts := commandPaneCounts(panes)
	byPID := map[int]processinfo.Process{42: process}
	harnessByPID := map[int]registry.Harness{42: registry.HarnessCodex}
	if _, _, ok := multiplexerPaneProcess(panes[0], []processinfo.Process{process}, byPID, harnessByPID, counts); ok {
		t.Fatal("ambiguous command/cwd fallback correlated a pane")
	}
}
