package client_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/aht/internal/broker"
	"github.com/zigai/aht/internal/brokerapi"
	"github.com/zigai/aht/pkg/client"
	"github.com/zigai/aht/pkg/registry"
)

func TestClientListFallsBackToDurableRegistry(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(storePath)
	if _, err := store.Observe(t.Context(), runningObservation("fallback")); err != nil {
		t.Fatal(err)
	}

	sessions, err := client.New(client.Config{StorePath: storePath}).List(
		t.Context(),
		registry.Filter{Presence: registry.PresenceLive},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "fallback" {
		t.Fatalf("List() = %#v, want fallback session", sessions)
	}
}

//nolint:cyclop // Verifies broker startup, streaming updates, and cancellation in one test.
func TestClientWatchYieldsRealtimeRevisions(t *testing.T) {
	t.Parallel()

	storePath, err := shortStatePath()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(storePath)
		_ = os.Remove(brokerapi.SocketPath(storePath))
	})
	store, err := registry.OpenMemoryStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan struct{})
	serverErrors := make(chan error, 1)
	server := broker.New(broker.Options{
		Store:      store,
		SocketPath: brokerapi.SocketPath(storePath),
		Ready:      ready,
	})
	go func() { serverErrors <- server.Serve(ctx) }()
	select {
	case <-ready:
	case err := <-serverErrors:
		t.Fatalf("broker exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("broker did not become ready")
	}

	watchClient := client.New(client.Config{StorePath: storePath})
	watchErrors := make(chan error, 1)
	snapshots := make(chan registry.StateSnapshot, 2)
	go func() {
		watchErrors <- watchClient.Watch(ctx, registry.Filter{}, func(snapshot registry.StateSnapshot) error {
			snapshots <- snapshot
			return nil
		})
	}()

	initial := receiveSnapshot(t, snapshots)
	if len(initial.Sessions) != 0 {
		t.Fatalf("initial snapshot = %#v, want no sessions", initial)
	}
	if _, err := watchClient.Observe(t.Context(), runningObservation("live")); err != nil {
		t.Fatal(err)
	}
	updated := receiveSnapshot(t, snapshots)
	if updated.Revision <= initial.Revision || len(updated.Sessions) != 1 {
		t.Fatalf("updated snapshot = %#v, want a newer revision with one session", updated)
	}

	cancel()
	if err := <-watchErrors; err != nil {
		t.Fatalf("Watch() after cancellation = %v, want nil", err)
	}
	if err := <-serverErrors; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve() after cancellation = %v", err)
	}
}

func receiveSnapshot(t *testing.T, snapshots <-chan registry.StateSnapshot) registry.StateSnapshot {
	t.Helper()

	select {
	case snapshot := <-snapshots:
		return snapshot
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for state snapshot")
		return registry.StateSnapshot{}
	}
}

func runningObservation(sessionID string) registry.Observation {
	presence := registry.PresenceLive
	activity := registry.ActivityRunning
	return registry.Observation{
		Source:     registry.ObservationSourceNative,
		Evidence:   registry.ObservationEvidenceNativeEvent,
		Harness:    registry.HarnessCodex,
		Identity:   registry.ObservationIdentity{SessionID: sessionID},
		Presence:   &presence,
		Activity:   &activity,
		ObservedAt: time.Now().UTC(),
	}
}

// shortStatePath keeps the derived broker socket below Darwin's Unix socket path limit.
func shortStatePath() (string, error) {
	stateFile, err := os.CreateTemp("", "aht-client-*.json")
	if err != nil {
		return "", fmt.Errorf("creating temporary state path: %w", err)
	}
	path := stateFile.Name()
	if err := stateFile.Close(); err != nil {
		return "", fmt.Errorf("closing temporary state file: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("removing temporary state file: %w", err)
	}
	return path, nil
}
