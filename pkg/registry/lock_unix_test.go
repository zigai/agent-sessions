//go:build linux || darwin

package registry

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreMutationsCancelWhileLockHeld(t *testing.T) {
	for _, operation := range []string{"observe", "gc", "reset"} {
		t.Run(operation, func(t *testing.T) {
			store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			lock, err := openStoreLock(t.Context(), store.Path()+".lock")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			t.Cleanup(func() {
				if err := lock.Close(); err != nil {
					t.Error(err)
				}
			})
			go func() {
				switch operation {
				case "observe":
					_, err = store.Observe(ctx, Observation{
						Harness: HarnessCodex, Source: ObservationSourceNative,
						Evidence: ObservationEvidenceNativeEvent, Identity: ObservationIdentity{SessionID: "blocked"},
					})
				case "gc":
					_, err = store.GC(ctx, 0)
				case "reset":
					_, err = store.Reset(ctx)
				}
				done <- err
			}()
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("mutation error = %v, want cancellation", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("mutation did not stop while the store lock remained held")
			}
		})
	}
}

func TestStoreLockDeadlineWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	lock, err := openStoreLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	second, err := openStoreLock(ctx, path)
	if second != nil {
		if closeErr := second.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error = %v, want deadline exceeded", err)
	}
}
