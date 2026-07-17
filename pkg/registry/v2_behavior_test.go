package registry

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

//nolint:cyclop // generation test covers terminal, ignored, resume, and process transitions
func TestV2NativeTerminalAndResumeReduction(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	base := time.Now().UTC().Add(-time.Minute)
	start := NativeLifecycleStart
	idle := ActivityIdle
	session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, Lifecycle: &start, Activity: &idle, ObservedAt: base})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceUnknown || session.Activity == nil || *session.Activity != ActivityIdle {
		t.Fatalf("start reduction: %#v", session)
	}
	end := NativeLifecycleEnd
	session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, Lifecycle: &end, ObservedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceGone || session.Activity != nil {
		t.Fatalf("end reduction: %#v", session)
	}
	processPresent := true
	process := &ProcessIdentity{PID: 12, StartIdentity: "boot:12"}
	session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, ProcessPresent: &processPresent, Process: process, ObservedAt: base.Add(1500 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceGone {
		t.Fatalf("post-terminal process revived session: %#v", session)
	}
	running := ActivityRunning
	session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, Activity: &running, ObservedAt: base.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceGone || session.Activity != nil || session.Observations.Native.Lifecycle == nil || *session.Observations.Native.Lifecycle != NativeLifecycleEnd {
		t.Fatalf("post-terminal activity changed evidence: %#v", session)
	}
	resume := NativeLifecycleResume
	session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, NativeEvent: "test", Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, Lifecycle: &resume, ObservedAt: base.Add(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceUnknown {
		t.Fatalf("resume should await process evidence: %#v", session)
	}
	session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceProcess, Evidence: ObservationEvidenceProcessPresence, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "s"}, ProcessPresent: &processPresent, Process: process, ObservedAt: base.Add(4 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceLive {
		t.Fatalf("process did not bind resumed generation: %#v", session)
	}
}

func TestV2CatalogCreationPolicyAndJSON(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	at := time.Now().UTC().Add(-time.Minute)
	catalog := &CatalogMetadata{Current: false, CWD: "/history"}
	_, err := store.ObserveBatch(context.Background(), []Observation{{Source: ObservationSourceCatalog, Evidence: ObservationEvidenceCatalogMetadata, Harness: HarnessGoose, Identity: ObservationIdentity{SessionID: "old"}, Catalog: catalog, ObservedAt: at}})
	if err != nil {
		t.Fatal(err)
	}
	if sessions, listErr := store.List(context.Background(), Filter{}); listErr != nil || len(sessions) != 0 {
		t.Fatalf("historical catalog created a record: %v %#v", listErr, sessions)
	}
	catalog.Current = true
	session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceCatalog, Evidence: ObservationEvidenceCatalogMetadata, Harness: HarnessClaude, Identity: ObservationIdentity{SessionID: "current"}, Catalog: catalog, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != PresenceUnknown || session.Activity == nil || *session.Activity != ActivityUnknown {
		t.Fatalf("catalog reduction: %#v", session)
	}
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["state"]; ok {
		t.Fatalf("legacy state in wire: %s", data)
	}
	if wire["schema_version"] != float64(storeSchemaVersion) {
		t.Fatalf("schema version: %s", data)
	}
}
