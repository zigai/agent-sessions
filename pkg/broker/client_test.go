package broker_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zigai/aht/pkg/broker"
	"github.com/zigai/aht/pkg/registry"
)

var errOther = errors.New("other error")

var _ registry.Store = (*broker.Client)(nil)

func TestClientOfflineReturnsUnavailable(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")
	client := broker.NewClientForSocket(socketPath)

	if client.SocketPath() != socketPath {
		t.Fatalf("SocketPath() = %q, want %q", client.SocketPath(), socketPath)
	}

	err := client.Ping(t.Context())
	if !broker.IsUnavailable(err) {
		t.Fatalf("Ping() err = %v, want ErrUnavailable", err)
	}

	_, err = client.List(t.Context(), registry.Filter{})
	if !broker.IsUnavailable(err) {
		t.Fatalf("List() err = %v, want ErrUnavailable", err)
	}

	_, err = client.Subscribe(t.Context(), registry.Filter{})
	if !broker.IsUnavailable(err) {
		t.Fatalf("Subscribe() err = %v, want ErrUnavailable", err)
	}
}

func TestIsUnavailable(t *testing.T) {
	t.Parallel()

	if !broker.IsUnavailable(broker.ErrUnavailable) {
		t.Fatal("IsUnavailable(ErrUnavailable) = false, want true")
	}

	wrapped := fmt.Errorf("something: %w", broker.ErrUnavailable)
	if !broker.IsUnavailable(wrapped) {
		t.Fatal("IsUnavailable(wrapped) = false, want true")
	}
	if broker.IsUnavailable(errOther) {
		t.Fatal("IsUnavailable(other) = true, want false")
	}
}
