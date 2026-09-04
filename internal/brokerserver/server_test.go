//go:build linux || darwin

package brokerserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/aht/pkg/broker"
	"github.com/zigai/aht/pkg/registry"
)

//nolint:cyclop // One end-to-end scenario verifies transport, permissions, and streaming.
func TestServerStreamsEffectiveStateChanges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path, err := shortStatePath()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(broker.SocketPath(path))
	})
	store, err := registry.OpenMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	server := New(Options{
		Store:      store,
		SocketPath: broker.SocketPath(path),
		Ready:      ready,
	})
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(ctx) }()

	select {
	case err := <-serverErrors:
		t.Fatalf("server exited before readiness: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-ready:
	}

	info, err := os.Stat(broker.SocketPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}

	client := broker.NewClient(path)
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

// shortStatePath keeps the derived socket below Darwin's Unix socket path limit.
func shortStatePath() (string, error) {
	stateFile, err := os.CreateTemp("", "aht-broker-state-")
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

type brokerAdversityFixture struct {
	path   string
	store  *registry.MemoryStore
	cancel context.CancelFunc
	done   <-chan error
}

func TestServer_RejectsProtocolAdversity(t *testing.T) {
	fixture := startBrokerAdversityFixture(t)
	tests := []struct {
		name        string
		payload     []byte
		wantCode    string
		wantMessage string
	}{
		{
			name:        "malformed JSON",
			payload:     []byte("{not-json}\n"),
			wantCode:    "invalid_json",
			wantMessage: "invalid character",
		},
		{
			name:        "unsupported version",
			payload:     marshalBrokerRequest(t, broker.Request{Version: broker.ProtocolVersion + 1, ID: "wrong-version", Method: broker.MethodPing}),
			wantCode:    "invalid_request",
			wantMessage: "unsupported broker protocol version",
		},
		{
			name: "oversized batch",
			payload: marshalBrokerRequest(t, broker.Request{
				Version:      broker.ProtocolVersion,
				ID:           "large-batch",
				Method:       broker.MethodObserveBatch,
				Observations: make([]registry.Observation, maxBatchObservations+1),
			}),
			wantCode:    "invalid_request",
			wantMessage: "too many observations",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := sendRawBrokerRequest(t, fixture.path, test.payload)
			if response.Type != "error" || response.Error == nil || response.Error.Code != test.wantCode || !strings.Contains(response.Error.Message, test.wantMessage) {
				t.Fatalf("response = %#v, want %q containing %q", response, test.wantCode, test.wantMessage)
			}
			state, err := fixture.store.State(t.Context(), registry.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if state.Revision != 1 || len(state.Sessions) != 0 {
				t.Fatalf("invalid request mutated state: %#v", state)
			}
		})
	}

	if err := broker.NewClient(fixture.path).Ping(t.Context()); err != nil {
		t.Fatalf("broker did not serve a valid request after adversity: %v", err)
	}
	stopBrokerAdversityFixture(t, fixture)
}

func TestServer_BoundsSlowSubscriber(t *testing.T) {
	fixture := startBrokerAdversityFixture(t)
	connection := dialRawBroker(t, fixture.path)
	defer func() { _ = connection.Close() }()
	if unixConnection, ok := connection.(*net.UnixConn); ok {
		_ = unixConnection.SetReadBuffer(1)
	}
	subscribe := marshalBrokerRequest(t, broker.Request{
		Version: broker.ProtocolVersion,
		ID:      "slow-subscriber",
		Method:  broker.MethodSubscribe,
	})
	if _, err := connection.Write(subscribe); err != nil {
		t.Fatalf("write slow subscription: %v", err)
	}

	padding := strings.Repeat("x", 1<<20)
	presence := registry.PresenceLive
	for index := range 4 {
		_, err := fixture.store.Observe(t.Context(), registry.Observation{
			Source:      registry.ObservationSourceNative,
			Evidence:    registry.ObservationEvidenceNativeEvent,
			Harness:     registry.HarnessOmp,
			Identity:    registry.ObservationIdentity{SessionID: fmt.Sprintf("slow-%d", index)},
			Presence:    &presence,
			NativeEvent: "agent_start",
			Attributes:  map[string]string{"padding": padding},
			ObservedAt:  time.Now().UTC().Add(time.Duration(index) * time.Nanosecond),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	clientContext, cancelClient := context.WithTimeout(t.Context(), time.Second)
	defer cancelClient()
	client := broker.NewClient(fixture.path)
	if err := client.Ping(clientContext); err != nil {
		t.Fatalf("slow subscriber blocked another client: %v", err)
	}
	session, err := client.Observe(clientContext, registry.Observation{
		Source:      registry.ObservationSourceNative,
		Evidence:    registry.ObservationEvidenceNativeEvent,
		Harness:     registry.HarnessOmp,
		Identity:    registry.ObservationIdentity{SessionID: "healthy-client"},
		Presence:    &presence,
		NativeEvent: "agent_start",
		ObservedAt:  time.Now().UTC(),
	})
	if err != nil || session.SessionID != "healthy-client" {
		t.Fatalf("healthy client observe = %#v, %v", session, err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("disconnect slow subscriber: %v", err)
	}
	if err := client.Ping(clientContext); err != nil {
		t.Fatalf("disconnected subscriber affected broker: %v", err)
	}
	stopBrokerAdversityFixture(t, fixture)
}

func TestServer_ShutsDownCleanly(t *testing.T) {
	fixture := startBrokerAdversityFixture(t)
	connection := dialRawBroker(t, fixture.path)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "{"); err != nil {
		t.Fatalf("write partial handshake: %v", err)
	}

	fixture.cancel()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case err := <-fixture.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-timer.C:
		t.Fatal("broker did not close a partial-handshake connection during shutdown")
	}
	if _, err := os.Stat(broker.SocketPath(fixture.path)); !os.IsNotExist(err) {
		t.Fatalf("broker socket remains after shutdown: %v", err)
	}
}

func startBrokerAdversityFixture(t *testing.T) brokerAdversityFixture {
	t.Helper()
	path, err := shortStatePath()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(broker.SocketPath(path))
	})
	store, err := registry.OpenMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	server := New(Options{Store: store, SocketPath: broker.SocketPath(path), Ready: ready})
	go func() { done <- server.Serve(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("server exited before readiness: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-ready:
	}
	return brokerAdversityFixture{path: path, store: store, cancel: cancel, done: done}
}

func stopBrokerAdversityFixture(t *testing.T, fixture brokerAdversityFixture) {
	t.Helper()
	fixture.cancel()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case err := <-fixture.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-timer.C:
		t.Fatal("broker did not stop within its connection deadline")
	}
}

func marshalBrokerRequest(t *testing.T, request broker.Request) []byte {
	t.Helper()
	//nolint:musttag // Request defines the complete public JSON protocol schema.
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return append(payload, '\n')
}

func dialRawBroker(t *testing.T, storePath string) net.Conn {
	t.Helper()
	dialer := net.Dialer{Timeout: time.Second}
	connection, err := dialer.DialContext(t.Context(), "unix", broker.SocketPath(storePath))
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func sendRawBrokerRequest(t *testing.T, storePath string, payload []byte) broker.Response {
	t.Helper()
	connection := dialRawBroker(t, storePath)
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	var response broker.Response
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		t.Fatalf("decode broker response: %v", err)
	}
	return response
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
		SocketPath: broker.SocketPath(path),
		Ready:      ready,
	})
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(ctx) }()
	<-ready

	client := broker.NewClient(path)
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
