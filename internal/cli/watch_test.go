package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestDiffWatchEventsSeparatesPresenceAndActivity(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	oldActivity := registry.ActivityIdle
	newActivity := registry.ActivityWaiting
	old := registry.Session{ID: "s", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Activity: &oldActivity, UpdatedAt: at}
	next := old
	next.Activity = &newActivity
	next.ActivityChangedAt = at.Add(time.Minute)
	next.UpdatedAt = at.Add(time.Minute)
	events := diffWatchEvents(map[string]registry.Session{"s": old}, map[string]registry.Session{"s": next}, at.Add(2*time.Minute))
	if len(events) != 1 || events[0].Action != watchActionActivityChanged {
		t.Fatalf("unexpected activity events: %#v", events)
	}
	if events[0].PreviousActivity == nil || *events[0].PreviousActivity != registry.ActivityIdle {
		t.Fatalf("missing previous activity: %#v", events[0])
	}
}

func TestDiffWatchEventsReportsMultiplexerLocationChanges(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	activity := registry.ActivityIdle
	old := registry.Session{ID: "s", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Activity: &activity, UpdatedAt: at}
	next := old
	next.Multiplexer = registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "work", PaneID: "terminal_7"}
	next.UpdatedAt = at.Add(time.Second)
	events := diffWatchEvents(map[string]registry.Session{"s": old}, map[string]registry.Session{"s": next}, at.Add(2*time.Second))
	if len(events) != 1 || events[0].Action != watchActionLocationChanged || events[0].Multiplexer != "zellij:work:terminal_7" {
		t.Fatalf("multiplexer location events = %#v", events)
	}
}

func TestWatchJSONModeEmitsJSONLinesOnlyWhenRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	activity := registry.ActivityIdle
	if _, err := store.Observe(context.Background(), registry.Observation{Harness: registry.HarnessCodex, Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Identity: registry.ObservationIdentity{SessionID: "watch-json"}, Activity: &activity, ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := &application{storePath: path, outputJSON: true, stdout: &stdout, stderr: &bytes.Buffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.runWatch(ctx, watchOptions{ready: ready})
	}()
	<-ready
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("watch JSONL lines = %d: %q", len(lines), stdout.String())
	}
	var event watchEvent
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil || event.Action != watchActionSnapshot {
		t.Fatalf("watch JSONL event = %q, %v", lines[0], err)
	}
}

func TestWatchDefaultsToHumanTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	activity := registry.ActivityIdle
	if _, err := store.Observe(context.Background(), registry.Observation{Harness: registry.HarnessCodex, Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Identity: registry.ObservationIdentity{SessionID: "watch-human"}, Activity: &activity, ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := &application{storePath: path, stdout: &stdout, stderr: &bytes.Buffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.runWatch(ctx, watchOptions{ready: ready})
	}()
	<-ready
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "snapshot") || !strings.Contains(stdout.String(), "watch-human") || strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("watch default output = %q", stdout.String())
	}
}

func TestDiffWatchEventsReportsProcessTransitions(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	activity := registry.ActivityUnknown
	old := registry.Session{ID: "s", Harness: registry.HarnessClaude, Presence: registry.PresenceUnknown, Activity: &activity, UpdatedAt: at}
	next := old
	next.Presence = registry.PresenceLive
	next.PresenceChangedAt = at.Add(time.Second)
	next.UpdatedAt = at.Add(time.Second)
	next.Process = &registry.ProcessIdentity{PID: 42, StartIdentity: "boot:42"}
	events := diffWatchEvents(map[string]registry.Session{"s": old}, map[string]registry.Session{"s": next}, at.Add(2*time.Second))
	if len(events) != 2 || events[0].Action != watchActionPresenceChanged || events[1].Action != watchActionProcessBound {
		t.Fatalf("unexpected process bind events: %#v", events)
	}
}

func TestFormatWatchPlainUsesNullableActivity(t *testing.T) {
	t.Parallel()
	event := watchEvent{Time: time.Unix(0, 0), Action: watchActionRemoved, Harness: registry.HarnessCodex, Presence: registry.PresenceGone, Label: "gone"}
	if got := formatWatchPlainEvent(event); got == "" || got[len(got)-len("session=gone"):] != "session=gone" {
		t.Fatalf("unexpected watch plain format: %q", got)
	}
}
