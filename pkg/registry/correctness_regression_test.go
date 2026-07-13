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
