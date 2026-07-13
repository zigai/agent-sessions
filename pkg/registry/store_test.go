package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreBatchIsAtomicOnSourceConflict(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	at := time.Now().UTC().Add(-time.Minute)
	idle := ActivityIdle
	first, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "atomic"}, Activity: &idle, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	running := ActivityRunning
	_, err = store.ObserveBatch(context.Background(), []Observation{
		{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "atomic"}, Activity: &running, ObservedAt: at.Add(time.Second)},
		{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "atomic"}, Activity: &idle, ObservedAt: at.Add(time.Second)},
	})
	if !errors.Is(err, ErrObservationConflict) {
		t.Fatalf("expected source conflict, got %v", err)
	}
	session, err := store.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity == nil || *session.Activity != ActivityIdle {
		t.Fatalf("failed batch partially committed: %#v", session)
	}
}

func TestStoreListFiltersIndependentDimensions(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	at := time.Now().UTC().Add(-time.Minute)
	start := NativeLifecycleStart
	idle := ActivityIdle
	for _, observation := range []Observation{
		{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "live-idle"}, Lifecycle: &start, Activity: &idle, ObservedAt: at},
		{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessClaude, Identity: ObservationIdentity{SessionID: "unknown"}, ObservedAt: at},
	} {
		if _, err := store.Observe(context.Background(), observation); err != nil {
			t.Fatal(err)
		}
	}
	present := true
	if _, err := store.Observe(context.Background(), Observation{Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "live-idle"}, ProcessPresent: &present, Process: &ProcessIdentity{PID: 7, StartIdentity: "boot:7"}, ObservedAt: at.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	end := NativeLifecycleEnd
	if _, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessClaude, Identity: ObservationIdentity{SessionID: "unknown"}, Lifecycle: &end, ObservedAt: at.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	live, err := store.List(context.Background(), Filter{Presence: PresenceLive, Activity: ActivityIdle})
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ID == "" {
		t.Fatalf("unexpected independent filter result: %#v", live)
	}
	gone, err := store.List(context.Background(), Filter{Presence: PresenceGone})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0].Activity != nil {
		t.Fatalf("gone activity must be null: %#v", gone)
	}
}

func TestStorePersistsSchemaV2Envelope(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := NewFileStore(path)
	at := time.Now().UTC().Add(-time.Minute)
	if _, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "json"}, ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version  int                `json:"schema_version"`
		Sessions map[string]Session `json:"sessions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != 2 || len(envelope.Sessions) != 1 {
		t.Fatalf("unexpected schema envelope: %#v", envelope)
	}
}

func TestStoreRejectsSchemaV1(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewFileStore(path).List(context.Background(), Filter{})
	var unsupported *UnsupportedSchemaError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected unsupported schema, got %v", err)
	}
}
