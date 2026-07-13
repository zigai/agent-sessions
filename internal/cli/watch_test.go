package cli

import (
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
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
