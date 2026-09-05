package aht_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zigai/aht/pkg/aht"
	"github.com/zigai/aht/pkg/registry"
)

func TestInvalidModeDoesNotCreateState(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "state", "sessions.json")
	c := aht.New(aht.Config{Mode: "typo", StorePath: storePath})
	if _, err := c.List(t.Context(), aht.Filter{}); !errors.Is(err, aht.ErrInvalidMode) {
		t.Fatalf("List error = %v, want ErrInvalidMode", err)
	}
	if _, err := os.Stat(filepath.Dir(storePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid mode touched state directory: %v", err)
	}
}

func TestAhtPackageTypesAndDefaults(t *testing.T) {
	t.Parallel()

	if aht.PresenceLive != registry.PresenceLive {
		t.Fatalf("PresenceLive = %q, want %q", aht.PresenceLive, registry.PresenceLive)
	}
	if aht.HarnessPi != registry.HarnessPi {
		t.Fatalf("HarnessPi = %q, want %q", aht.HarnessPi, registry.HarnessPi)
	}
	if aht.ActivityRunning != registry.ActivityRunning {
		t.Fatalf("ActivityRunning = %q, want %q", aht.ActivityRunning, registry.ActivityRunning)
	}

	c := aht.New(aht.Config{
		StorePath:  filepath.Join(t.TempDir(), "sessions.json"),
		SocketPath: filepath.Join(t.TempDir(), "nonexistent.sock"),
		Mode:       aht.ModeRealtimeOnly,
	})

	if c.Mode() != aht.ModeRealtimeOnly {
		t.Fatalf("Mode() = %q, want %q", c.Mode(), aht.ModeRealtimeOnly)
	}

	_, err := c.List(t.Context(), aht.Filter{Presence: aht.PresenceLive})
	if !aht.IsUnavailable(err) {
		t.Fatalf("List() err = %v, want ErrUnavailable", err)
	}
}
