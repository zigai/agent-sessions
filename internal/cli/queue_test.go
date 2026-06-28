package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/internal/reportqueue"
	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestQueuedReportDrainsToCustomStore(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "custom-state.json")
	payload := `{"session_id":"queued-claude","transcript_path":"/tmp/.claude/projects/queued.jsonl","cwd":"/repo","hook_event_name":"UserPromptSubmit"}`
	var out bytes.Buffer
	cmd := NewRootCommand(&out, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommandName,
		"--harness", "claude",
		"--state", "running",
		"--source", "claude-hook",
		"--attribute", "agent_sessions_integration=claude-hook",
		"--raw-stdin",
		"--queue",
		"--quiet",
		"--no-tmux",
	})
	cmd.SetIn(strings.NewReader(payload))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("queued report command failed: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("expected quiet queued report output, got %q", out.String())
	}
	if sessions := listQueuedTestSessions(t, storePath); len(sessions) != 0 {
		t.Fatalf("expected queue not to update store before drain, got %#v", sessions)
	}

	drain := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	drain.SetArgs([]string{storeFlag, storePath, drainQueueCommandName})
	if err := drain.Execute(); err != nil {
		t.Fatalf("drain queue command failed: %v", err)
	}

	sessions := listQueuedTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one drained session, got %#v", sessions)
	}
	session := sessions[0]
	if session.SessionID != "queued-claude" || session.State != registry.StateRunning {
		t.Fatalf("unexpected drained session: %#v", session)
	}
	if len(session.RawPayload) == 0 {
		t.Fatal("expected raw payload to be preserved after queued drain")
	}
}

func TestQueuedObservedAtPreventsOlderRetryFromOverwritingNewerState(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	app := &application{}
	queue := reportqueue.New(storePath)
	older := queuedTestEnvelope(storePath, "ordered", registry.StateRunning, time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC))
	newer := queuedTestEnvelope(storePath, "ordered", registry.StateWaiting, older.CreatedAt.Add(time.Second))

	if err := app.processQueuedReport(context.Background(), queue, newer); err != nil {
		t.Fatalf("processing newer report: %v", err)
	}
	if err := app.processQueuedReport(context.Background(), queue, older); err != nil {
		t.Fatalf("processing older report: %v", err)
	}

	sessions := listQueuedTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", sessions)
	}
	if sessions[0].State != registry.StateWaiting {
		t.Fatalf("expected newer waiting state to survive older retry, got %#v", sessions[0])
	}
}

func TestQueuedReportFallsBackToMinimalTmuxContext(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storePath := filepath.Join(root, "state.json")
	socket := filepath.Join(root, "missing-tmux")
	app := &application{}
	queue := reportqueue.New(storePath)
	envelope := queuedTestEnvelope(storePath, "tmux", registry.StateRunning, time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC))
	envelope.Runtime.Env = map[string]string{
		"TMUX":      socket + ",123,0",
		"TMUX_PANE": "%9",
	}
	if err := app.processQueuedReport(context.Background(), queue, envelope); err != nil {
		t.Fatalf("processing queued report: %v", err)
	}

	sessions := listQueuedTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", sessions)
	}
	if !sessions[0].Tmux.Inside || sessions[0].Tmux.PaneID != "%9" || sessions[0].Tmux.ServerSocket != socket {
		t.Fatalf("expected minimal tmux context from env, got %#v", sessions[0].Tmux)
	}
}

func TestQueuedReportUsesCachedTmuxWhenCollectionFallsBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storePath := filepath.Join(root, "state.json")
	socket := filepath.Join(root, "missing-tmux")
	app := &application{}
	queue := reportqueue.New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	envelope := queuedTestEnvelope(storePath, "tmux-cached", registry.StateRunning, now)
	envelope.Runtime.Env = map[string]string{
		"TMUX":      socket + ",123,0",
		"TMUX_PANE": "%9",
	}
	richTmux := registry.TmuxContext{
		Inside:          true,
		ServerSocket:    socket,
		SessionID:       "$1",
		SessionName:     "work",
		WindowID:        "@2",
		WindowIndex:     "0",
		WindowName:      "editor",
		PaneID:          "%9",
		PaneIndex:       "1",
		PaneCurrentPath: "/repo",
		PanePID:         1234,
		PaneTTY:         "/dev/ttys001",
	}
	if err := queue.StoreTmuxContext(context.Background(), richTmux, time.Now().UTC()); err != nil {
		t.Fatalf("storing cached tmux context: %v", err)
	}

	if err := app.processQueuedReport(context.Background(), queue, envelope); err != nil {
		t.Fatalf("processing queued report: %v", err)
	}

	sessions := listQueuedTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", sessions)
	}
	if sessions[0].Tmux.SessionName != "work" || sessions[0].Tmux.WindowName != "editor" || sessions[0].Tmux.PaneCurrentPath != "/repo" {
		t.Fatalf("expected cached tmux details, got %#v", sessions[0].Tmux)
	}
}

func TestQueuedReportPreservesExplicitNullRawPayload(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	app := &application{}
	queue := reportqueue.New(storePath)
	envelope := queuedTestEnvelope(storePath, "raw-null", registry.StateRunning, time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC))
	envelope.Report.RawPayload = []byte("null")
	envelope.RawPayloadSet = true
	if err := app.processQueuedReport(context.Background(), queue, envelope); err != nil {
		t.Fatalf("processing queued report: %v", err)
	}

	sessions := listQueuedTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", sessions)
	}
	if string(sessions[0].RawPayload) != "null" {
		t.Fatalf("expected explicit null raw payload to survive, got %q", sessions[0].RawPayload)
	}
}

func queuedTestEnvelope(storePath string, sessionID string, state registry.State, observedAt time.Time) reportqueue.Envelope {
	return reportqueue.Envelope{
		Version:   reportqueue.EnvelopeVersion,
		ID:        reportqueue.NewEnvelopeID(observedAt),
		CreatedAt: observedAt,
		StorePath: storePath,
		Kind:      reportqueue.KindReport,
		Report: reportqueue.ReportFromRegistry(registry.Report{
			Harness:    registry.HarnessClaude,
			State:      state,
			SessionID:  sessionID,
			CWD:        "/repo",
			Source:     "test",
			Confidence: "hook",
			ObservedAt: observedAt,
		}),
	}
}

func listQueuedTestSessions(t *testing.T, storePath string) []registry.Session {
	t.Helper()
	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{Harness: registry.HarnessClaude})
	if err != nil {
		t.Fatalf("listing sessions: %v", err)
	}

	return sessions
}
