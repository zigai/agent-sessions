package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestDiffWatchEventsClassifiesSessionChanges(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	previousEvent := watchTestSession("event", registry.StateRunning, base)
	previousEvent.LastEvent = "Before"
	previousEvent.LastEventAt = base
	previous := map[string]registry.Session{
		"state":            watchTestSession("state", registry.StateRunning, base),
		"event":            previousEvent,
		watchActionUpdated: watchTestSession(watchActionUpdated, registry.StateRunning, base),
		watchActionRemoved: watchTestSession(watchActionRemoved, registry.StateWaiting, base),
	}

	nextState := watchTestSession("state", registry.StateWaiting, base.Add(time.Minute))
	nextState.StateChangedAt = base.Add(time.Minute)
	nextEvent := watchTestSession("event", registry.StateRunning, base.Add(2*time.Minute))
	nextEvent.LastEvent = "After"
	nextEvent.LastEventAt = base.Add(2 * time.Minute)
	nextUpdated := watchTestSession(watchActionUpdated, registry.StateRunning, base.Add(3*time.Minute))
	nextUpdated.CWD = "/repo/next"
	next := map[string]registry.Session{
		"state":            nextState,
		"event":            nextEvent,
		watchActionUpdated: nextUpdated,
		watchActionAdded:   watchTestSession(watchActionAdded, registry.StateIdle, base.Add(4*time.Minute)),
	}

	events := diffWatchEvents(previous, next, base.Add(5*time.Minute))
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %#v", events)
	}

	byID := make(map[string]watchEvent, len(events))
	for _, event := range events {
		byID[event.ID] = event
	}

	if byID[watchActionAdded].Action != watchActionAdded {
		t.Fatalf("expected added action, got %#v", byID[watchActionAdded])
	}
	if byID[watchActionRemoved].Action != watchActionRemoved || !byID[watchActionRemoved].Time.Equal(base.Add(5*time.Minute)) {
		t.Fatalf("expected removed action at observed time, got %#v", byID[watchActionRemoved])
	}
	if byID["state"].Action != watchActionStateChanged || byID["state"].PreviousState != registry.StateRunning {
		t.Fatalf("expected state_changed from running, got %#v", byID["state"])
	}
	if byID["event"].Action != watchActionEventChanged || byID["event"].PreviousEvent != "Before" {
		t.Fatalf("expected event_changed from previous event, got %#v", byID["event"])
	}
	if byID[watchActionUpdated].Action != watchActionUpdated {
		t.Fatalf("expected updated action, got %#v", byID[watchActionUpdated])
	}
}

func TestDiffWatchSummaryEventsClassifiesSummaryChanges(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 17, 10, 5, 0, 0, time.UTC)
	previous := map[string]registry.Summary{
		"$1":      watchTestSummary("$1", testTmuxSessionName, 1, 1),
		"$remove": watchTestSummary("$remove", "remove", 1, 1),
	}
	next := map[string]registry.Summary{
		"$1": watchTestSummary("$1", testTmuxSessionName, 2, 3),
		"$2": watchTestSummary("$2", "next", 1, 1),
	}

	events := diffWatchSummaryEvents(previous, next, observedAt)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %#v", events)
	}

	byLabel := make(map[string]watchSummaryEvent, len(events))
	for _, event := range events {
		byLabel[event.Label] = event
	}

	if byLabel["next"].Action != watchActionAdded {
		t.Fatalf("expected added summary event, got %#v", byLabel["next"])
	}
	if byLabel["remove"].Action != watchActionRemoved {
		t.Fatalf("expected removed summary event, got %#v", byLabel["remove"])
	}
	if byLabel[testTmuxSessionName].Action != watchActionUpdated || byLabel[testTmuxSessionName].Total != 3 {
		t.Fatalf("expected updated summary event, got %#v", byLabel[testTmuxSessionName])
	}
}

func TestSnapshotWatchEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	older := watchTestSession("older", registry.StateRunning, now.Add(-2*time.Minute))
	newer := watchTestSession("newer", registry.StateWaiting, now.Add(-time.Minute))

	events := snapshotWatchEvents([]registry.Session{newer, older}, now)
	if len(events) != 2 {
		t.Fatalf("expected two snapshot events, got %#v", events)
	}
	if events[0].ID != "older" || events[1].ID != "newer" {
		t.Fatalf("expected snapshot events sorted by time, got %#v", events)
	}

	empty := snapshotWatchEvents(nil, now)
	if len(empty) != 1 || empty[0].Action != watchActionSnapshotEmpty || !empty[0].Time.Equal(now) {
		t.Fatalf("expected snapshot_empty event, got %#v", empty)
	}
}

func TestSnapshotWatchSummaryEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	events := snapshotWatchSummaryEvents([]registry.Summary{
		watchTestSummary("$2", "next", 1, 1),
		watchTestSummary("$1", testTmuxSessionName, 2, 3),
	}, now)
	if len(events) != 2 {
		t.Fatalf("expected two summary snapshot events, got %#v", events)
	}
	if events[0].Label != "next" || events[1].Label != testTmuxSessionName {
		t.Fatalf("expected summary snapshot events sorted by label, got %#v", events)
	}

	empty := snapshotWatchSummaryEvents(nil, now)
	if len(empty) != 1 || empty[0].Action != watchActionSnapshotEmpty || !empty[0].Time.Equal(now) {
		t.Fatalf("expected summary snapshot_empty event, got %#v", empty)
	}
}

func TestFormatWatchPlainEvent(t *testing.T) {
	t.Parallel()

	event := watchEvent{
		Time:          time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:        watchActionStateChanged,
		Harness:       registry.HarnessCodex,
		State:         registry.StateWaiting,
		PreviousState: registry.StateRunning,
		Label:         "abc",
		Event:         "PreToolUse",
		CWD:           "/repo with space",
		Tmux:          "work:2:%3",
	}

	got := formatWatchPlainEvent(event)
	want := `2026-06-17T10:02:00Z state_changed codex waiting session=abc prev=running event=PreToolUse cwd="/repo with space" tmux=work:2:%3`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatWatchSummaryPlainEvent(t *testing.T) {
	t.Parallel()

	event := watchSummaryEvent{
		Time:            time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:          watchActionUpdated,
		TmuxSessionID:   "$1",
		TmuxSessionName: "work tree",
		Label:           "work tree",
		Active:          2,
		Total:           3,
		Running:         1,
		Waiting:         1,
		Idle:            1,
		Unknown:         0,
		Exited:          0,
	}

	got := formatWatchSummaryPlainEvent(event)
	want := `2026-06-17T10:02:00Z updated tmux="work tree" active=2 total=3 running=1 waiting=1 idle=1 unknown=0 exited=0`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatWatchTableEvent(t *testing.T) {
	t.Parallel()

	event := watchEvent{
		Time:          time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:        watchActionStateChanged,
		Harness:       registry.HarnessCodex,
		State:         registry.StateWaiting,
		PreviousState: registry.StateRunning,
		Label:         "abc",
		Event:         "PreToolUse",
		CWD:           "/repo with space",
		Tmux:          "work:2:%3",
	}

	got := formatWatchTableEvent(event)
	want := `2026-06-17T10:02:00Z  state_changed   codex       waiting   abc                       prev=running event=PreToolUse cwd="/repo with space" tmux=work:2:%3`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatWatchSummaryTableEvent(t *testing.T) {
	t.Parallel()

	event := watchSummaryEvent{
		Time:            time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:          watchActionUpdated,
		TmuxSessionID:   "$1",
		TmuxSessionName: testTmuxSessionName,
		Label:           testTmuxSessionName,
		Active:          2,
		Total:           3,
		Running:         1,
		Waiting:         1,
		Idle:            1,
		Unknown:         0,
		Exited:          0,
	}

	got := formatWatchSummaryTableEvent(event)
	want := `2026-06-17T10:02:00Z  updated         work                      2/3      1        1        1        0        0`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestWriteWatchEventJSONL(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	app := &application{
		outputJSON: true,
		stdout:     &output,
		stderr:     &bytes.Buffer{},
	}
	event := watchEvent{
		Time:    time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:  watchActionAdded,
		ID:      "codex-1",
		Harness: registry.HarnessCodex,
		State:   registry.StateRunning,
		Label:   "abc",
	}

	if err := app.writeWatchEvents([]watchEvent{event}, watchFormatJSON); err != nil {
		t.Fatalf("writeWatchEvents returned error: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(output.String()), "[") {
		t.Fatalf("expected newline-delimited JSON object, got %q", output.String())
	}

	var decoded watchEvent
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON event, got %v: %q", err, output.String())
	}
	if decoded.Action != watchActionAdded || decoded.ID != "codex-1" {
		t.Fatalf("unexpected decoded event: %#v", decoded)
	}
}

func TestWriteWatchSummaryEventJSONL(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	app := &application{
		storePath:  "",
		outputJSON: true,
		stdout:     &output,
		stderr:     &bytes.Buffer{},
	}
	event := watchSummaryEvent{
		Time:            time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC),
		Action:          watchActionAdded,
		TmuxSessionID:   "$1",
		TmuxSessionName: testTmuxSessionName,
		Label:           testTmuxSessionName,
		Active:          1,
		Total:           1,
		Running:         1,
		Waiting:         0,
		Idle:            0,
		Unknown:         0,
		Exited:          0,
	}

	if err := app.writeWatchSummaryEvents([]watchSummaryEvent{event}, watchFormatJSON); err != nil {
		t.Fatalf("writeWatchSummaryEvents returned error: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(output.String()), "[") {
		t.Fatalf("expected newline-delimited JSON object, got %q", output.String())
	}

	var decoded watchSummaryEvent
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON event, got %v: %q", err, output.String())
	}
	if decoded.Action != watchActionAdded || decoded.Label != testTmuxSessionName || decoded.Active != 1 {
		t.Fatalf("unexpected decoded event: %#v", decoded)
	}
}

func TestValidateWatchTextFormat(t *testing.T) {
	t.Parallel()

	for _, format := range []string{watchFormatTable, watchFormatPlain} {
		if err := validateWatchTextFormat(format); err != nil {
			t.Fatalf("expected %s format to be valid: %v", format, err)
		}
	}
	for _, format := range []string{watchFormatJSON, "yaml"} {
		if err := validateWatchTextFormat(format); err == nil {
			t.Fatalf("expected %s to be an invalid watch text format", format)
		}
	}
}

func TestWatchRejectsFormatWithGlobalJSON(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{
		"--json",
		storeFlag, filepath.Join(t.TempDir(), "state.json"),
		listCommandName,
		"--watch",
		"--format", "plain",
		"--no-snapshot",
	})

	err := cmd.Execute()
	if !errors.Is(err, errWatchFormatJSONConflict) {
		t.Fatalf("expected --json/--format conflict, got %v", err)
	}
}

func TestWatchRejectsJSONFormatWithoutGlobalJSON(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{
		storeFlag, filepath.Join(t.TempDir(), "state.json"),
		listCommandName,
		"--watch",
		"--format", "json",
		"--no-snapshot",
	})

	err := cmd.Execute()
	if !errors.Is(err, errInvalidWatchFormat) {
		t.Fatalf("expected json format rejection, got %v", err)
	}
}

func TestWatchDoesNotCreateMissingStateOrLock(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	output := &safeBuffer{}
	app := &application{
		storePath: storePath,
		stdout:    output,
		stderr:    &safeBuffer{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.runWatch(ctx, watchOptions{
			noSnapshot: true,
			format:     watchFormatTable,
			debounce:   time.Millisecond,
			ready:      ready,
		})
	}()

	waitForWatchReady(t, ready, done)
	cancel()
	waitForWatchDone(t, done)

	assertPathMissing(t, storePath)
	assertPathMissing(t, storePath+".lock")
	if output.String() != "" {
		t.Fatalf("expected no output with --no-snapshot, got %q", output.String())
	}
}

func TestWatchRejectsMissingStateDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storeDir := filepath.Join(root, "missing")
	storePath := filepath.Join(storeDir, "state.json")
	app := &application{
		storePath: storePath,
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
	}

	err := app.runWatch(context.Background(), watchOptions{noSnapshot: true})
	if !errors.Is(err, errWatchStateDirectoryMissing) {
		t.Fatalf("expected missing state directory error, got %v", err)
	}
	assertPathMissing(t, storeDir)
}

func TestWatchReportsFileStoreChanges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "state.json")
	output := &safeBuffer{}
	stderr := &safeBuffer{}
	app := &application{
		storePath: storePath,
		stdout:    output,
		stderr:    stderr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.runWatch(ctx, watchOptions{
			noSnapshot: true,
			debounce:   10 * time.Millisecond,
			ready:      ready,
		})
	}()
	defer cancel()
	waitForWatchReady(t, ready, done)

	store := registry.NewFileStore(storePath)
	store.SetNowForTest(func() time.Time { return now })
	if _, err := store.Report(context.Background(), registry.Report{
		Harness:    registry.HarnessCodex,
		State:      registry.StateRunning,
		SessionID:  "abc",
		CWD:        "/repo",
		Event:      "UserPromptSubmit",
		ObservedAt: now,
	}); err != nil {
		t.Fatalf("reporting session: %v", err)
	}

	waitForOutput(t, output, "added           codex       running   abc")
	cancel()
	waitForWatchDone(t, done)
	if stderr.String() != "" {
		t.Fatalf("expected no watch warnings, got %q", stderr.String())
	}
}

func TestWatchSummaryReportsFileStoreChanges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "state.json")
	output := &safeBuffer{}
	stderr := &safeBuffer{}
	app := &application{
		storePath:  storePath,
		outputJSON: false,
		stdout:     output,
		stderr:     stderr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.runWatch(ctx, watchOptions{
			filter: registry.Filter{
				Harness:     "",
				State:       "",
				TmuxSession: "",
				ActiveOnly:  false,
			},
			summary:    true,
			noSnapshot: true,
			format:     watchFormatTable,
			formatSet:  false,
			debounce:   10 * time.Millisecond,
			now:        nil,
			ready:      ready,
		})
	}()
	defer cancel()
	waitForWatchReady(t, ready, done)

	store := registry.NewFileStore(storePath)
	store.SetNowForTest(func() time.Time { return now })
	if _, err := store.Report(context.Background(), registry.Report{
		Harness:    registry.HarnessCodex,
		State:      registry.StateRunning,
		SessionID:  "abc",
		ObservedAt: now,
		Tmux: registry.TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     testTmuxSessionName,
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
	}); err != nil {
		t.Fatalf("reporting session: %v", err)
	}

	waitForOutput(t, output, "added           work")
	cancel()
	waitForWatchDone(t, done)
	if stderr.String() != "" {
		t.Fatalf("expected no watch warnings, got %q", stderr.String())
	}
}

func watchTestSession(id string, state registry.State, updatedAt time.Time) registry.Session {
	return registry.Session{
		ID:             id,
		Harness:        registry.HarnessCodex,
		State:          state,
		SessionID:      id,
		CWD:            "/repo",
		CreatedAt:      updatedAt.Add(-time.Minute),
		UpdatedAt:      updatedAt,
		LastSeenAt:     updatedAt,
		StateChangedAt: updatedAt,
	}
}

func watchTestSummary(sessionID string, sessionName string, active int, total int) registry.Summary {
	return registry.Summary{
		TmuxSessionID:   sessionID,
		TmuxSessionName: sessionName,
		Total:           total,
		Active:          active,
		Running:         active,
		Waiting:         0,
		Idle:            total - active,
		Unknown:         0,
		Exited:          0,
	}
}

type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *safeBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	written, err := b.buffer.Write(data)
	if err != nil {
		return written, fmt.Errorf("writing safe buffer: %w", err)
	}

	return written, nil
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buffer.String()
}

func waitForWatchReady(t *testing.T, ready <-chan struct{}, done <-chan error) {
	t.Helper()

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("watch exited before ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watch to become ready")
	}
}

func waitForWatchDone(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watch to stop")
	}
}

func waitForOutput(t *testing.T, output *safeBuffer, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q in output %q", want, output.String())
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if err == nil {
		t.Fatalf("expected %s to be missing", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing path %s, got %v", path, err)
	}
}
