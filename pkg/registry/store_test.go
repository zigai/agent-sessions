package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	assertStoreGCKeepsOldLiveSession(t, store, session.ID, now)
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

func assertStoreGCKeepsOldLiveSession(t *testing.T, store *FileStore, sessionID string, now time.Time) {
	t.Helper()

	ctx := context.Background()
	store.SetNowForTest(func() time.Time { return now.Add(2 * time.Hour) })

	result, err := store.GC(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if result.Deleted != 0 || result.Remaining != 1 {
		t.Fatalf("expected live session to remain, got %#v", result)
	}

	session, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if session.State != StateRunning {
		t.Fatalf("expected running state, got %q", session.State)
	}
	if !session.StateChangedAt.Equal(now) {
		t.Fatalf("expected state_changed_at to remain %s, got %s", now, session.StateChangedAt)
	}
	if !session.EndedAt.IsZero() {
		t.Fatalf("running session should not have ended_at, got %s", session.EndedAt)
	}
}

func TestFileStoreGCDeletesOldExitedSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })
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

	store.SetNowForTest(func() time.Time { return now.Add(2 * time.Hour) })
	result, err := store.GC(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if result.Deleted != 1 || result.Remaining != 0 {
		t.Fatalf("expected exited session to be deleted, got %#v", result)
	}
	if _, getErr := store.Get(ctx, exited.ID); !errors.Is(getErr, ErrSessionNotFound) {
		t.Fatalf("expected deleted session to be missing, got %v", getErr)
	}
}

func TestFileStoreResetClearsSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(storePath)
	store.SetNowForTest(func() time.Time { return now })
	session := reportRunningCodexSession(t, store, ctx, now)

	resetAt := now.Add(time.Minute)
	store.SetNowForTest(func() time.Time { return resetAt })
	result, err := store.Reset(ctx)
	if err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if result.Cleared != 1 || result.Remaining != 0 {
		t.Fatalf("expected one cleared session and no remaining sessions, got %#v", result)
	}

	sessions, err := store.List(ctx, Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("List after reset returned error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected reset store to be empty, got %#v", sessions)
	}
	if _, getErr := store.Get(ctx, session.ID); !errors.Is(getErr, ErrSessionNotFound) {
		t.Fatalf("expected reset session to be missing, got %v", getErr)
	}

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("reading reset store: %v", err)
	}
	var snap snapshot
	if unmarshalErr := json.Unmarshal(data, &snap); unmarshalErr != nil {
		t.Fatalf("reset store is not valid JSON: %v", unmarshalErr)
	}
	if snap.Version != storeVersion || !snap.UpdatedAt.Equal(resetAt) || len(snap.Sessions) != 0 {
		t.Fatalf("unexpected reset snapshot: %#v", snap)
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

func TestReportPathThenSessionIDMergesSameLogicalSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return now })

	firstReport := testReport(HarnessCodex, StateIdle)
	firstReport.SessionPath = "/tmp/codex/session.jsonl"
	first, err := store.Report(ctx, firstReport)
	if err != nil {
		t.Fatalf("first Report returned error: %v", err)
	}

	store.SetNowForTest(func() time.Time { return now.Add(time.Second) })
	secondReport := testReport(HarnessCodex, StateRunning)
	secondReport.SessionID = "session-123"
	secondReport.SessionPath = "/tmp/codex/session.jsonl"
	second, err := store.Report(ctx, secondReport)
	if err != nil {
		t.Fatalf("second Report returned error: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("expected canonical id to upgrade from path key to session id key, both were %q", second.ID)
	}

	sessions, err := store.List(ctx, filterByHarness(HarnessCodex))
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("same harness/path should be one logical session, got %#v", sessions)
	}
	if sessions[0].SessionID != "session-123" || sessions[0].SessionPath != "/tmp/codex/session.jsonl" {
		t.Fatalf("expected merged identities, got %#v", sessions[0])
	}
}

func TestReportMergesSeparateIdentityRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return base })
	pathOnlyReport := testReport(HarnessCodex, StateIdle)
	pathOnlyReport.SessionPath = "/tmp/codex/session.jsonl"
	pathOnly, err := store.Report(ctx, pathOnlyReport)
	if err != nil {
		t.Fatalf("path Report returned error: %v", err)
	}

	store.SetNowForTest(func() time.Time { return base.Add(time.Second) })
	idOnlyReport := testReport(HarnessCodex, StateRunning)
	idOnlyReport.SessionID = "session-123"
	idOnly, err := store.Report(ctx, idOnlyReport)
	if err != nil {
		t.Fatalf("id Report returned error: %v", err)
	}
	if idOnly.ID == pathOnly.ID {
		t.Fatalf("setup expected separate records before overlapping identity report")
	}

	store.SetNowForTest(func() time.Time { return base.Add(2 * time.Second) })
	mergedReport := testReport(HarnessCodex, StateWaiting)
	mergedReport.SessionID = "session-123"
	mergedReport.SessionPath = "/tmp/codex/session.jsonl"
	merged, err := store.Report(ctx, mergedReport)
	if err != nil {
		t.Fatalf("merge Report returned error: %v", err)
	}

	sessions, err := store.List(ctx, filterByHarness(HarnessCodex))
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected duplicate identity records to coalesce, got %#v", sessions)
	}
	if sessions[0].ID != merged.ID || sessions[0].State != StateWaiting {
		t.Fatalf("unexpected merged session: returned=%#v listed=%#v", merged, sessions[0])
	}
}

func TestReportSupersedesOtherHarnessInSameTmuxPane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	store.SetNowForTest(func() time.Time { return base })

	grokReport := testReport(HarnessGrok, StateIdle)
	grokReport.SessionID = "grok-session"
	grokReport.Tmux = testSeshTmuxPane()
	grok, err := store.Report(ctx, grokReport)
	requireNoStoreError(t, err, "grok Report returned error")

	piObservedAt := base.Add(time.Minute)
	store.SetNowForTest(func() time.Time { return piObservedAt })
	piReport := testReport(HarnessPi, StateIdle)
	piReport.SessionPath = "/tmp/pi-session.jsonl"
	piReport.Tmux = testSeshTmuxPane()
	piSession, err := store.Report(ctx, piReport)
	requireNoStoreError(t, err, "pi Report returned error")
	if piSession.State != StateIdle {
		t.Fatalf("expected current pane occupant to remain idle, got %q", piSession.State)
	}

	storedGrok, err := store.Get(ctx, grok.ID)
	requireNoStoreError(t, err, "Get grok returned error")
	assertSupersededPaneSession(t, storedGrok, piObservedAt)
	assertSeshPaneSummary(t, store, ctx)
}

func TestStalePaneReportDoesNotSupersedeNewerOccupant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))

	piObservedAt := base.Add(time.Minute)
	store.SetNowForTest(func() time.Time { return piObservedAt })
	piReport := testReport(HarnessPi, StateIdle)
	piReport.SessionPath = "/tmp/pi-session.jsonl"
	piReport.Tmux = testSeshTmuxPane()
	piSession, err := store.Report(ctx, piReport)
	requireNoStoreError(t, err, "pi Report returned error")

	staleGrokReport := testReport(HarnessGrok, StateIdle)
	staleGrokReport.SessionID = "grok-session"
	staleGrokReport.Tmux = testSeshTmuxPane()
	staleGrokReport.ObservedAt = base
	staleGrok, err := store.Report(ctx, staleGrokReport)
	requireNoStoreError(t, err, "stale grok Report returned error")
	if staleGrok.State != StateExited {
		t.Fatalf("expected stale incoming pane occupant to be exited, got %q", staleGrok.State)
	}

	storedPi, err := store.Get(ctx, piSession.ID)
	requireNoStoreError(t, err, "Get pi returned error")
	if storedPi.State != StateIdle {
		t.Fatalf("newer pane occupant should remain idle, got %q", storedPi.State)
	}
	if !staleGrok.EndedAt.Equal(piObservedAt) {
		t.Fatalf("expected stale report to end at newer occupant time %s, got %s", piObservedAt, staleGrok.EndedAt)
	}
}

func TestReportObservedAtPreventsStaleStateRegression(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))

	runningReport := testReport(HarnessKilo, StateRunning)
	runningReport.SessionID = "session-123"
	runningReport.Event = "agent.start"
	runningReport.ObservedAt = base
	_, err := store.Report(ctx, runningReport)
	if err != nil {
		t.Fatalf("running Report returned error: %v", err)
	}
	idleReport := testReport(HarnessKilo, StateIdle)
	idleReport.SessionID = "session-123"
	idleReport.Event = "agent.end"
	idleReport.ObservedAt = base.Add(time.Second)
	idle, err := store.Report(ctx, idleReport)
	if err != nil {
		t.Fatalf("idle Report returned error: %v", err)
	}
	staleReport := testReport(HarnessKilo, StateRunning)
	staleReport.SessionID = "session-123"
	staleReport.Event = "agent.start"
	staleReport.ObservedAt = base.Add(500 * time.Millisecond)
	stale, err := store.Report(ctx, staleReport)
	if err != nil {
		t.Fatalf("stale Report returned error: %v", err)
	}

	if stale.State != StateIdle || stale.LastEvent != "agent.end" {
		t.Fatalf("stale observed report should not regress lifecycle, got state=%q event=%q", stale.State, stale.LastEvent)
	}
	if !stale.StateChangedAt.Equal(idle.StateChangedAt) || !stale.LastEventAt.Equal(idle.LastEventAt) {
		t.Fatalf("stale observed report changed lifecycle timestamps: before=%#v after=%#v", idle, stale)
	}
	if stale.UpdatedAt.Before(idle.UpdatedAt) || stale.LastSeenAt.Before(idle.LastSeenAt) {
		t.Fatalf("stale observed report moved update times backwards: before=%#v after=%#v", idle, stale)
	}
}

func TestSortSessionsUsesNumericTmuxIndexes(t *testing.T) {
	t.Parallel()

	sessions := []Session{
		testSessionWithTmux("w10", "10", "1"),
		testSessionWithTmux("w2", "2", "1"),
		testSessionWithTmux("p10", "2", "10"),
		testSessionWithTmux("p2", "2", "2"),
	}

	sortSessions(sessions)
	got := []string{sessions[0].ID, sessions[1].ID, sessions[2].ID, sessions[3].ID}
	want := []string{"w2", "p2", "p10", "w10"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected numeric tmux order %#v, got %#v", want, got)
	}
}

func requireNoStoreError(t *testing.T, err error, message string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

func assertSupersededPaneSession(t *testing.T, session Session, endedAt time.Time) {
	t.Helper()
	if session.State != StateExited {
		t.Fatalf("expected old pane occupant to be exited, got %q", session.State)
	}
	if !session.StateChangedAt.Equal(endedAt) || !session.EndedAt.Equal(endedAt) || !session.LastSeenAt.Equal(endedAt) {
		t.Fatalf(
			"expected old pane occupant to end at %s, got state_changed_at=%s ended_at=%s last_seen_at=%s",
			endedAt,
			session.StateChangedAt,
			session.EndedAt,
			session.LastSeenAt,
		)
	}
}

func assertSeshPaneSummary(t *testing.T, store *FileStore, ctx context.Context) {
	t.Helper()
	summaries, err := store.SummaryByTmuxSession(ctx, Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "sesh",
		ActiveOnly:  false,
	})
	requireNoStoreError(t, err, "SummaryByTmuxSession returned error")
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %#v", summaries)
	}
	if summaries[0].Idle != 1 || summaries[0].Exited != 1 {
		t.Fatalf("expected one open idle occupant and one exited stale occupant, got %#v", summaries[0])
	}
}

func testSeshTmuxPane() TmuxContext {
	return TmuxContext{
		Inside:          true,
		SessionID:       "$1",
		SessionName:     "sesh",
		WindowID:        "@1",
		WindowIndex:     "1",
		WindowName:      "zsh",
		PaneID:          "%21",
		PaneIndex:       "1",
		PaneCurrentPath: "/repo",
		PanePID:         1234,
		PaneTTY:         "/dev/pts/1",
		ClientTTY:       "/dev/pts/0",
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

func testReport(harness Harness, state State) Report {
	return Report{
		Harness:       harness,
		State:         state,
		SessionID:     "",
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
		Event:         "",
		Attributes:    nil,
		RawPayload:    nil,
		ObservedAt:    time.Time{},
	}
}

func filterByHarness(harness Harness) Filter {
	return Filter{
		Harness:     harness,
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	}
}

func testSessionWithTmux(id string, windowIndex string, paneIndex string) Session {
	return Session{
		ID:            id,
		Harness:       HarnessCodex,
		State:         "",
		SessionID:     "",
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
			SessionName:     "work",
			WindowID:        "",
			WindowIndex:     windowIndex,
			WindowName:      "",
			PaneID:          "",
			PaneIndex:       paneIndex,
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
		Source:         "",
		Confidence:     "",
		LastEvent:      "",
		Attributes:     nil,
		RawPayload:     nil,
		CreatedAt:      time.Time{},
		UpdatedAt:      time.Time{},
		LastSeenAt:     time.Time{},
		StateChangedAt: time.Time{},
		LastEventAt:    time.Time{},
		EndedAt:        time.Time{},
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

func TestSummaryWithOptionsKeepsOldLiveSessionsOpen(t *testing.T) {
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
	})
	if err != nil {
		t.Fatalf("SummaryByTmuxSessionWithOptions returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}
	if summaries[0].Active != 1 || summaries[0].Running != 1 || summaries[0].Total != 1 {
		t.Fatalf("expected live summary to remain open, got %#v", summaries[0])
	}

	stored, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if stored.State != StateRunning {
		t.Fatalf("summary should not rewrite state, got %q", stored.State)
	}
}
