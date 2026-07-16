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

func TestCatalogOnlyCreationIsClaudeCurrentOnly(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	at := time.Now().UTC().Add(-time.Minute)
	old := &CatalogMetadata{Current: false, CWD: "/history"}
	if _, err := store.ObserveBatch(context.Background(), []Observation{{Source: ObservationSourceCatalog, Evidence: ObservationEvidenceCatalogMetadata, Harness: HarnessGoose, Identity: ObservationIdentity{SessionID: "old"}, Catalog: old, ObservedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if sessions, err := store.List(context.Background(), Filter{}); err != nil || len(sessions) != 0 {
		t.Fatalf("historical catalog created record: %#v %v", sessions, err)
	}
	current := &CatalogMetadata{Current: true, CWD: "/work"}
	created, err := store.Observe(context.Background(), Observation{Source: ObservationSourceCatalog, Evidence: ObservationEvidenceCatalogMetadata, Harness: HarnessClaude, Identity: ObservationIdentity{SessionID: "current"}, Catalog: current, ObservedAt: at.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if created.Presence != PresenceUnknown || created.Activity == nil || *created.Activity != ActivityUnknown {
		t.Fatalf("unexpected catalog-only record: %#v", created)
	}
}

func boolPtr(value bool) *bool { return &value }
