//go:build linux || darwin

package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/aht/v2/pkg/brokerapi"
	"github.com/zigai/aht/v2/pkg/registry"
)

//nolint:cyclop // One end-to-end scenario verifies transport, permissions, and streaming.
func TestServerStreamsEffectiveStateChanges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := registry.OpenMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	server := New(Options{
		Store:      store,
		SocketPath: brokerapi.SocketPath(path),
		Ready:      ready,
	})
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(ctx) }()

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-ready:
	}

	info, err := os.Stat(brokerapi.SocketPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}

	client := brokerapi.NewClient(path)
	subscription, err := client.Subscribe(ctx, registry.Filter{Presence: registry.PresenceLive})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	initial := receiveSnapshot(t, ctx, subscription.Snapshots)
	if initial.Revision != 1 || len(initial.Sessions) != 0 {
		t.Fatalf("initial snapshot = %#v, want empty revision 1", initial)
	}

	presence := registry.PresenceLive
	running := registry.ActivityRunning
	observation := registry.Observation{
		Source:      registry.ObservationSourceNative,
		Evidence:    registry.ObservationEvidenceNativeEvent,
		Harness:     registry.HarnessOmp,
		Identity:    registry.ObservationIdentity{SessionID: "broker-live"},
		Presence:    &presence,
		Activity:    &running,
		NativeEvent: "agent_start",
		Attributes:  map[string]string{"aht_integration": "omp-extension"},
		ObservedAt:  time.Now().UTC(),
	}
	if _, err := client.Observe(ctx, observation); err != nil {
		t.Fatal(err)
	}

	update := receiveSnapshot(t, ctx, subscription.Snapshots)
	if update.Revision != 2 || len(update.Sessions) != 1 || update.Sessions[0].Activity == nil || *update.Sessions[0].Activity != registry.ActivityRunning {
		t.Fatalf("update snapshot = %#v, want one running session at revision 2", update)
	}

	cancel()
	select {
	case err := <-serverErrors:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func receiveSnapshot(
	t *testing.T,
	ctx context.Context,
	snapshots <-chan registry.StateSnapshot,
) registry.StateSnapshot {
	t.Helper()

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return registry.StateSnapshot{}
	case snapshot, ok := <-snapshots:
		if !ok {
			t.Fatal("subscription closed before snapshot")
		}
		return snapshot
	}
}

func BenchmarkBrokerObserveRoundTrip(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	path := filepath.Join(b.TempDir(), "state.json")
	store, err := registry.OpenMemoryStore(path)
	if err != nil {
		b.Fatal(err)
	}
	ready := make(chan struct{})
	server := New(Options{
		Store:      store,
		SocketPath: brokerapi.SocketPath(path),
		Ready:      ready,
	})
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(ctx) }()
	<-ready

	client := brokerapi.NewClient(path)
	running := registry.ActivityRunning
	sequence := uint64(0)
	observation := registry.Observation{
		Source:      registry.ObservationSourceNative,
		Evidence:    registry.ObservationEvidenceNativeEvent,
		Harness:     registry.HarnessOmp,
		Identity:    registry.ObservationIdentity{SessionID: "benchmark"},
		Activity:    &running,
		Sequence:    &sequence,
		NativeEvent: "agent_start",
		Attributes:  map[string]string{"aht_integration": "omp-extension"},
		ObservedAt:  time.Now().UTC(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sequence++
		observation.ObservedAt = observation.ObservedAt.Add(time.Nanosecond)
		if _, err := client.Observe(ctx, observation); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	cancel()
	if err := <-serverErrors; err != nil {
		b.Fatal(err)
	}
}
