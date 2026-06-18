package registry

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreReportAndList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })

	session := reportRunningCodexSession(t, store, ctx, now)
	assertUnknownReportPreservesRunning(t, store, ctx, now)
	assertStoreListAndSummary(t, store, ctx)
	assertStoreGCMarksStale(t, store, session.ID, now)
}

func reportRunningCodexSession(t *testing.T, store *FileStore, ctx context.Context, now time.Time) Session {
	t.Helper()

	session, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateRunning,
		SessionID:     "session-1",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "/repo",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux: TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     "work",
			WindowID:        "",
			WindowIndex:     "2",
			WindowName:      "",
			PaneID:          "%3",
			PaneIndex:       "",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
		Source:     "",
		Confidence: "",
		Event:      "UserPromptSubmit",
		Attributes: nil,
		RawPayload: nil,
		ObservedAt: time.Time{},
	})
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}
	if session.ID == "" {
		t.Fatal("expected registry id")
	}
	if session.State != StateRunning {
		t.Fatalf("expected running state, got %q", session.State)
	}
	if len(session.ResumeCommand) != 0 {
		t.Fatalf("registry store should not derive resume commands, got %#v", session.ResumeCommand)
	}
	if !session.StateChangedAt.Equal(now) {
		t.Fatalf("expected state_changed_at %s, got %s", now, session.StateChangedAt)
	}
	if session.LastEvent != "UserPromptSubmit" || !session.LastEventAt.Equal(now) {
		t.Fatalf("unexpected event fields: event=%q at=%s", session.LastEvent, session.LastEventAt)
	}
	if !session.EndedAt.IsZero() {
		t.Fatalf("running session should not have ended_at, got %s", session.EndedAt)
	}

	return session
}

func assertUnknownReportPreservesRunning(t *testing.T, store *FileStore, ctx context.Context, now time.Time) {
	t.Helper()

	store.SetNowForTest(func() time.Time { return now.Add(time.Minute) })
	updated, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateUnknown,
		SessionID:     "session-1",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux: TmuxContext{
			Inside:          false,
			SessionID:       "",
			SessionName:     "",
			WindowID:        "",
			WindowIndex:     "",
			WindowName:      "",
			PaneID:          "",
			PaneIndex:       "",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
		Source:     "",
		Confidence: "",
		Event:      "session.updated",
		Attributes: nil,
		RawPayload: nil,
		ObservedAt: time.Time{},
	})
	if err != nil {
		t.Fatalf("Report update returned error: %v", err)
	}
	if updated.State != StateRunning {
		t.Fatalf("unknown report should not erase running state, got %q", updated.State)
	}
	if !updated.StateChangedAt.Equal(now) {
		t.Fatalf("state_changed_at should remain %s, got %s", now, updated.StateChangedAt)
	}
	if updated.LastEvent != "session.updated" || !updated.LastEventAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected last event fields: event=%q at=%s", updated.LastEvent, updated.LastEventAt)
	}
}

func assertStoreListAndSummary(t *testing.T, store *FileStore, ctx context.Context) {
	t.Helper()

	sessions, err := store.List(ctx, Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "",
		ActiveOnly:  true,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one active session, got %d", len(sessions))
	}

	summaries, err := store.SummaryByTmuxSession(ctx, Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("SummaryByTmuxSession returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}
	if summaries[0].Active != 1 || summaries[0].Total != 1 {
		t.Fatalf("unexpected summary counts: %#v", summaries[0])
	}
}

func assertStoreGCMarksStale(t *testing.T, store *FileStore, sessionID string, now time.Time) {
	t.Helper()

	ctx := context.Background()
	store.SetNowForTest(func() time.Time { return now.Add(2 * time.Hour) })

	result, err := store.GC(ctx, time.Hour, 0)
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if result.MarkedStale != 1 {
		t.Fatalf("expected one stale session, got %#v", result)
	}

	stale, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if stale.State != StateStale {
		t.Fatalf("expected stale state, got %q", stale.State)
	}
	if !stale.StateChangedAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("expected stale state_changed_at to be gc time, got %s", stale.StateChangedAt)
	}
	if !stale.EndedAt.IsZero() {
		t.Fatalf("stale session should not have ended_at, got %s", stale.EndedAt)
	}
}

func TestFileStoreEndedAtFollowsExitedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })
	reportRunningCodexSession(t, store, ctx, now)

	exitedAt := now.Add(5 * time.Minute)
	store.SetNowForTest(func() time.Time { return exitedAt })
	exited, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateExited,
		SessionID:     "session-1",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux:          emptyTmuxContext(),
		Source:        "",
		Confidence:    "",
		Event:         "Stop",
		Attributes:    nil,
		RawPayload:    nil,
		ObservedAt:    time.Time{},
	})
	if err != nil {
		t.Fatalf("exited Report returned error: %v", err)
	}
	if exited.State != StateExited || !exited.EndedAt.Equal(exitedAt) {
		t.Fatalf("expected exited session ended at %s, got state=%q ended_at=%s", exitedAt, exited.State, exited.EndedAt)
	}
	if !exited.StateChangedAt.Equal(exitedAt) {
		t.Fatalf("expected exited state_changed_at %s, got %s", exitedAt, exited.StateChangedAt)
	}

	resumedAt := now.Add(6 * time.Minute)
	store.SetNowForTest(func() time.Time { return resumedAt })
	resumed, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateIdle,
		SessionID:     "session-1",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux:          emptyTmuxContext(),
		Source:        "",
		Confidence:    "",
		Event:         "SessionStart",
		Attributes:    nil,
		RawPayload:    nil,
		ObservedAt:    time.Time{},
	})
	if err != nil {
		t.Fatalf("resumed Report returned error: %v", err)
	}
	if resumed.State != StateIdle || !resumed.EndedAt.IsZero() {
		t.Fatalf("expected resumed session with no ended_at, got state=%q ended_at=%s", resumed.State, resumed.EndedAt)
	}
	if !resumed.StateChangedAt.Equal(resumedAt) {
		t.Fatalf("expected resumed state_changed_at %s, got %s", resumedAt, resumed.StateChangedAt)
	}
}

func TestReportIdentityIgnoresTmuxWindowName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })

	first, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateRunning,
		SessionID:     "",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux: TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     "work",
			WindowID:        "@2",
			WindowIndex:     "1",
			WindowName:      "codex",
			PaneID:          "%3",
			PaneIndex:       "0",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
		Source:     "",
		Confidence: "",
		Event:      "",
		Attributes: nil,
		RawPayload: nil,
		ObservedAt: time.Time{},
	})
	if err != nil {
		t.Fatalf("first Report returned error: %v", err)
	}

	store.SetNowForTest(func() time.Time { return now.Add(time.Minute) })
	second, err := store.Report(ctx, Report{
		Harness:       HarnessCodex,
		State:         StateWaiting,
		SessionID:     "",
		SessionPath:   "",
		ResumeCommand: nil,
		CWD:           "",
		ProjectRoot:   "",
		PID:           0,
		PPID:          0,
		TTY:           "",
		Tmux: TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     "work",
			WindowID:        "@2",
			WindowIndex:     "1",
			WindowName:      "claude",
			PaneID:          "%3",
			PaneIndex:       "0",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
		Source:     "",
		Confidence: "",
		Event:      "",
		Attributes: nil,
		RawPayload: nil,
		ObservedAt: time.Time{},
	})
	if err != nil {
		t.Fatalf("second Report returned error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("window name change should not change session id: first=%q second=%q", first.ID, second.ID)
	}

	sessions, err := store.List(ctx, Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session after window rename, got %#v", sessions)
	}
	if sessions[0].Harness != HarnessCodex {
		t.Fatalf("window name should not affect harness, got %q", sessions[0].Harness)
	}
}

func emptyTmuxContext() TmuxContext {
	return TmuxContext{
		Inside:          false,
		SessionID:       "",
		SessionName:     "",
		WindowID:        "",
		WindowIndex:     "",
		WindowName:      "",
		PaneID:          "",
		PaneIndex:       "",
		PaneCurrentPath: "",
		PanePID:         0,
		PaneTTY:         "",
		ClientTTY:       "",
	}
}

func TestFileStoreGetMissing(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	_, err := store.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSummaryWithOptionsTreatsOldLiveSessionsStaleWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })
	session := reportRunningCodexSession(t, store, ctx, now)

	summaries, err := store.SummaryByTmuxSessionWithOptions(ctx, SummaryOptions{
		Filter: Filter{
			Harness:     "",
			State:       "",
			TmuxSession: "",
			ActiveOnly:  false,
		},
		StaleAfter: time.Hour,
		Now:        now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("SummaryByTmuxSessionWithOptions returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}
	if summaries[0].Active != 0 || summaries[0].Stale != 1 {
		t.Fatalf("expected read-side stale summary, got %#v", summaries[0])
	}

	stored, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if stored.State != StateRunning {
		t.Fatalf("read-side stale summary should not write state, got %q", stored.State)
	}
}
