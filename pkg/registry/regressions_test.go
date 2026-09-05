package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestSummarySeparatesServersAndCountsAllActivities(t *testing.T) {
	failed, interrupted := ActivityFailed, ActivityInterrupted
	sessions := []Session{
		{Presence: PresenceLive, Activity: &failed, Multiplexer: MultiplexerContext{Kind: MultiplexerTmux, ServerID: "first", SessionID: "$0"}},
		{Presence: PresenceLive, Activity: &interrupted, Multiplexer: MultiplexerContext{Kind: MultiplexerTmux, ServerID: "second", SessionID: "$0"}},
		{Presence: PresenceGone, Multiplexer: MultiplexerContext{Kind: MultiplexerTmux, ServerID: "first", SessionID: "$0"}},
	}
	summaries := summariesForSessions(sessions)
	if len(summaries) != 2 {
		t.Fatalf("summaries = %#v, want distinct servers", summaries)
	}
	if summaries[0].MultiplexerServerID != "first" || summaries[0].Failed != 1 || summaries[0].Gone != 1 || summaries[0].Total != 2 {
		t.Fatalf("first summary = %#v", summaries[0])
	}
	if summaries[1].MultiplexerServerID != "second" || summaries[1].Interrupted != 1 || summaries[1].Total != 1 {
		t.Fatalf("second summary = %#v", summaries[1])
	}
}

func TestSnapshotReadRejectsOversizedFileAndResetRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSnapshotBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path)
	if _, err := store.List(t.Context(), Filter{}); !errors.Is(err, ErrStoreTooLarge) {
		t.Fatalf("list error = %v, want oversized error", err)
	}
	if _, err := OpenMemoryStore(path); !errors.Is(err, ErrStoreTooLarge) {
		t.Fatalf("open error = %v, want oversized error", err)
	}
	if _, err := store.Reset(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sessions, err := store.List(t.Context(), Filter{}); err != nil || len(sessions) != 0 {
		t.Fatalf("reset sessions = %#v, error = %v", sessions, err)
	}
}

func TestMemoryStoreFlushWaitIsCancellable(t *testing.T) {
	store, err := OpenMemoryStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.flush <- struct{}{}
	defer func() { <-store.flush }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("flush error = %v, want canceled", err)
	}
}

func TestMemoryStoreConcurrentFlushPersistsLatestState(t *testing.T) {
	store, err := OpenMemoryStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for index := range 32 {
		group.Go(func() {
			activity := ActivityIdle
			_, err := store.Observe(t.Context(), Observation{
				Harness: HarnessCodex, Source: ObservationSourceNative,
				Evidence: ObservationEvidenceNativeEvent, Identity: ObservationIdentity{SessionID: strconv.Itoa(index)}, Activity: &activity,
			})
			if err != nil {
				t.Error(err)
				return
			}
			if err := store.Flush(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	persisted, err := NewFileStore(store.Path()).load()
	if err != nil {
		t.Fatal(err)
	}
	if !materialSnapshotsEqual(persisted, store.snapshot) {
		t.Fatalf("persisted %d sessions, want latest %d sessions", len(persisted.Sessions), len(store.snapshot.Sessions))
	}
}

func TestFileStoreCanceledMutationDoesNotCommit(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := store.withSnapshot(ctx, func(snap *snapshot) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mutation error = %v, want canceled", err)
	}
	if _, err := os.Stat(store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store stat error = %v, want no file committed", err)
	}
}
