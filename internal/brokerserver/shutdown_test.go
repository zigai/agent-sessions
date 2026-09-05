//go:build linux || darwin

package brokerserver

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/aht/pkg/broker"
	"github.com/zigai/aht/pkg/registry"
)

var errAcceptFailed = errors.New("accept failed")

type failingListener struct {
	net.Listener

	fail     <-chan struct{}
	accepted bool
}

func (l *failingListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		//nolint:wrapcheck // This listener delegates the first accept unchanged and injects only the next failure.
		return l.Listener.Accept()
	}
	<-l.fail
	return nil, errAcceptFailed
}

func TestAcceptFailureStopsActiveSubscriptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := registry.OpenMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", broker.SocketPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	fail := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := New(Options{Store: store, SocketPath: broker.SocketPath(path)})
	result := make(chan error, 1)
	go func() {
		result <- server.serve(ctx, &failingListener{Listener: listener, fail: fail})
	}()
	subscription, err := broker.NewClient(path).Subscribe(ctx, registry.Filter{})
	close(fail)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	<-subscription.Snapshots
	select {
	case err := <-result:
		if !errors.Is(err, errAcceptFailed) {
			t.Fatalf("Serve error = %v, want accept failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not join active connections after accept failure")
	}
	select {
	case _, ok := <-subscription.Snapshots:
		if ok {
			t.Fatal("unexpected snapshot after server exit")
		}
	case <-time.After(time.Second):
		t.Fatal("server returned with subscription connection still active")
	}
}
