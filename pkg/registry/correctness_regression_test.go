package registry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityReconciliationPrefersSessionPath(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	at := time.Now().UTC().Add(-time.Minute)
	first, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessClaude, Identity: ObservationIdentity{SessionPath: "/tmp/session.json"}, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Observe(context.Background(), Observation{Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence, Harness: HarnessClaude, Identity: ObservationIdentity{SessionPath: "/tmp/session.json"}, ProcessPresent: boolPtr(true), Process: &ProcessIdentity{PID: 41, StartIdentity: "boot:41"}, ObservedAt: at.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Presence != PresenceLive {
		t.Fatalf("observations did not reconcile: first=%#v second=%#v", first, second)
	}
}

//nolint:cyclop // reconciliation assertions cover identity, process, location, and row compaction
func TestNativeProcessIdentityReconcilesWithLiveTmuxSession(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute)
	path := "/tmp/pi-session.json"
	process := &ProcessIdentity{PID: 42, PPID: 10, ProcessGroupID: 42, StartIdentity: "boot:42", Executable: "/usr/bin/node", CWD: "/work", TTY: "/dev/pts/4"}
	idle := ActivityIdle
	identityOnly, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: ObservationIdentity{SessionPath: path},
		NativeEvent: "session_start", Activity: &idle, ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	live, err := store.Observe(ctx, Observation{
		Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence,
		Harness: HarnessPi, ProcessPresent: boolPtr(true), Process: process, ObservedAt: at.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	tmux := &TmuxContext{Inside: true, SessionName: "sesh", PaneID: "%81", PaneTTY: "/dev/pts/4"}
	if _, err := store.Observe(ctx, Observation{
		Source: ObservationSourceTmux, Evidence: ObservationEvidenceTmuxLocation,
		Harness: HarnessPi, Process: process, Tmux: tmux, ObservedAt: at.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	running := ActivityRunning
	reconciled, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: ObservationIdentity{SessionPath: path}, Process: process,
		NativeEvent: "agent_start", Activity: &running, ObservedAt: at.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.ID != live.ID || reconciled.ID == identityOnly.ID {
		t.Fatalf("native report reconciled to %q, want live process record %q (identity-only %q)", reconciled.ID, live.ID, identityOnly.ID)
	}
	if reconciled.Presence != PresenceLive || reconciled.Activity == nil || *reconciled.Activity != ActivityRunning || reconciled.SessionPath != path || reconciled.Tmux.PaneID != "%81" {
		t.Fatalf("reconciled session lost identity, activity, or tmux location: %#v", reconciled)
	}
	sessions, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != live.ID {
		t.Fatalf("provisional identity row survived reconciliation: %#v", sessions)
	}
}

//nolint:cyclop // switch assertions cover both historical rows and subsequent process matching
func TestNativeSessionSwitchOnSameProcessPreservesEndedHistory(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute)
	process := &ProcessIdentity{PID: 84, StartIdentity: "boot:84"}
	start := NativeLifecycleStart
	live := PresenceLive
	idle := ActivityIdle

	first, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: ObservationIdentity{SessionID: "old-session"},
		Lifecycle: &start, Presence: &live, Activity: &idle, Process: process,
		NativeEvent: "session_start", ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: ObservationIdentity{SessionID: "new-session"},
		Presence: &live, Activity: &idle, Process: process,
		NativeEvent: "session_start", ObservedAt: at.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.SessionID != "new-session" || second.Presence != PresenceLive {
		t.Fatalf("session switch overwrote identity: first=%#v second=%#v", first, second)
	}

	sessions, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("session history count = %d, want 2: %#v", len(sessions), sessions)
	}
	for _, session := range sessions {
		switch session.SessionID {
		case "old-session":
			if session.ID != first.ID || session.Presence != PresenceGone || session.Activity != nil {
				t.Fatalf("old session was not retired: %#v", session)
			}
		case "new-session":
			if session.ID != second.ID || session.Presence != PresenceLive {
				t.Fatalf("new session is not live: %#v", session)
			}
		default:
			t.Fatalf("unexpected session history row: %#v", session)
		}
	}

	present := true
	observed, err := store.Observe(ctx, Observation{
		Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence,
		Harness: HarnessPi, ProcessPresent: &present, Process: process,
		ObservedAt: at.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID != second.ID {
		t.Fatalf("process-only observation matched %q, want current session %q", observed.ID, second.ID)
	}
}

func TestNativeProcessIdentitySeedsObserverReconciliation(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute)
	process := &ProcessIdentity{PID: 42, StartIdentity: "boot:42"}
	tmux := &TmuxContext{Inside: true, SessionName: "dev", PaneID: "%4"}
	activity := ActivityRunning
	native, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: ObservationIdentity{SessionPath: "/tmp/pi-session.json"},
		Process: process, Tmux: tmux, NativeEvent: "agent_start", Activity: &activity, ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := store.Observe(ctx, Observation{
		Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence,
		Harness: HarnessPi, ProcessPresent: boolPtr(true), Process: process, ObservedAt: at.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID != native.ID || observed.Presence != PresenceLive || observed.Activity == nil || *observed.Activity != ActivityRunning || observed.Tmux.PaneID != "%4" {
		t.Fatalf("observer did not reconcile with native process identity: native=%#v observed=%#v", native, observed)
	}
}

func TestStaleNativeStartDoesNotReviveGoneSession(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute)
	identity := ObservationIdentity{SessionPath: "/tmp/pi-stale.json"}
	process := &ProcessIdentity{PID: 85, StartIdentity: "boot:85"}

	if _, err := store.Observe(ctx, Observation{
		Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence,
		Harness: HarnessPi, Identity: identity, ProcessPresent: boolPtr(true), Process: process,
		ObservedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Observe(ctx, Observation{
		Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence,
		Harness: HarnessPi, Identity: identity, ProcessPresent: boolPtr(false), Process: process,
		ObservedAt: at.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	start := NativeLifecycleStart
	live := PresenceLive
	idle := ActivityIdle
	observed, err := store.Observe(ctx, Observation{
		Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent,
		Harness: HarnessPi, Identity: identity, Lifecycle: &start, Presence: &live, Activity: &idle,
		Process: process, NativeEvent: "session_start", ObservedAt: at.Add(5 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Presence != PresenceGone || observed.Activity != nil {
		t.Fatalf("stale start revived gone session: %#v", observed)
	}
	if _, err := store.List(ctx, Filter{}); err != nil {
		t.Fatalf("stale start corrupted store: %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }
