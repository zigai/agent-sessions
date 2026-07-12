package reportqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

var (
	errTemporaryStoreFailure = errors.New("temporary store failure")
	errInvalidEnvelope       = errors.New("invalid envelope")
)

func TestQueueEnqueueDrainSuccess(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	envelope := testEnvelope(storePath, "abc", registry.StateRunning, now)

	result := enqueueTestEnvelope(t, queue, envelope, now)
	requireQueueItemMode(t, result.Path)

	var processed []Envelope
	drain, err := queue.Drain(context.Background(), testDrainOptions(now.Add(time.Second),
		func(_ context.Context, got Envelope) error {
			processed = append(processed, got)

			return nil
		},
	))
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if drain.Succeeded != 1 || len(processed) != 1 {
		t.Fatalf("expected one successful drain, got result=%#v processed=%d", drain, len(processed))
	}
	if processed[0].Report.SessionID != "abc" || !processed[0].Report.ObservedAt.Equal(now) {
		t.Fatalf("processed envelope mismatch: %#v", processed[0])
	}
	requireNoQueueFiles(t, queue.pendingDir(), "pending")
	requireNoQueueFiles(t, queue.processingDir(), "processing")
}

func TestQueueDrainRescansPendingBeforeUnlock(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "first", registry.StateRunning, now), now)

	var processed []string
	enqueuedSecond := false
	drain, err := queue.Drain(context.Background(), testDrainOptions(now.Add(time.Second),
		func(ctx context.Context, got Envelope) error {
			processed = append(processed, got.Report.SessionID)
			if got.Report.SessionID != "first" || enqueuedSecond {
				return nil
			}
			enqueuedSecond = true
			_, err := queue.Enqueue(
				ctx,
				testEnvelope(storePath, "second", registry.StateWaiting, now.Add(time.Nanosecond)),
				EnqueueOptions{Now: func() time.Time { return now.Add(time.Nanosecond) }},
			)

			return err
		},
	))
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if drain.Succeeded != 2 || len(processed) != 2 {
		t.Fatalf("expected active drain to pick up concurrent enqueue, got result=%#v processed=%#v", drain, processed)
	}
	requireNoQueueFiles(t, queue.pendingDir(), "pending")
}

func TestQueueDrainRetriesTransientError(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "abc", registry.StateRunning, now), now)

	drain, err := queue.Drain(context.Background(), testDrainOptions(now.Add(time.Second),
		func(context.Context, Envelope) error {
			return errTemporaryStoreFailure
		},
	))
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if drain.Retried != 1 {
		t.Fatalf("expected one retry, got %#v", drain)
	}
	pending := globQueueFiles(t, queue.pendingDir())
	if len(pending) != 1 {
		t.Fatalf("expected retry to return item to pending, got %#v", pending)
	}
	requireRetryMetadata(t, pending[0])
	requireQueueStatus(t, queue, 1, 0, 0)
}

func TestQueueDrainPermanentErrorMovesDead(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "abc", registry.StateRunning, now), now)

	drain, err := queue.Drain(context.Background(), testDrainOptions(now.Add(time.Second),
		func(context.Context, Envelope) error {
			return Permanent(errInvalidEnvelope)
		},
	))
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if drain.Dead != 1 {
		t.Fatalf("expected one dead-letter, got %#v", drain)
	}
	if dead := globQueueFiles(t, queue.deadDir()); len(dead) != 1 {
		t.Fatalf("expected one dead item, got %#v", dead)
	}
}

func TestQueueRecoversStaleProcessingLease(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	if err := ensureQueueDirs(queue); err != nil {
		t.Fatalf("ensureQueueDirs() error = %v", err)
	}
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	envelope := testEnvelope(storePath, "abc", registry.StateRunning, now)
	envelope.ID = "stale"
	envelope.ProcessingStartedAt = now.Add(-time.Hour)
	envelope.WorkerPID = 99999999
	processingPath := filepath.Join(queue.processingDir(), envelope.ID+".json")
	if err := writeJSONAtomic(processingPath, envelope); err != nil {
		t.Fatalf("writing processing envelope: %v", err)
	}

	options := testDrainOptions(now,
		func(context.Context, Envelope) error {
			return nil
		},
	)
	options.LeaseTimeout = time.Minute
	drain, err := queue.Drain(context.Background(), options)
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if drain.Recovered != 1 || drain.Succeeded != 1 {
		t.Fatalf("expected recovered item to drain, got %#v", drain)
	}
}

func TestTmuxCacheRespectsTTL(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	tmux := testTmuxContext()
	if err := queue.StoreTmuxContext(context.Background(), tmux, now); err != nil {
		t.Fatalf("StoreTmuxContext() error = %v", err)
	}
	if got, ok := queue.LookupTmuxContext(tmux, now.Add(10*time.Second), 30*time.Second); !ok || got.SessionName != "work" {
		t.Fatalf("expected fresh cache hit, got %#v ok=%v", got, ok)
	}
	if got, ok := queue.LookupTmuxContext(tmux, now.Add(time.Minute), 30*time.Second); ok || !got.Empty() {
		t.Fatalf("expected stale cache miss, got %#v ok=%v", got, ok)
	}
}

func TestStoreTmuxContextPrunesExpiredAndExcessEntries(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	expired := testTmuxContext()
	expired.PaneID = "%expired"
	if err := queue.StoreTmuxContext(context.Background(), expired, now); err != nil {
		t.Fatalf("StoreTmuxContext() error = %v", err)
	}

	for index := range maxTmuxCacheEntries + 1 {
		tmux := testTmuxContext()
		tmux.PaneID = fmt.Sprintf("%%%d", index)
		if err := queue.StoreTmuxContext(context.Background(), tmux, now.Add(defaultTmuxCacheTTL+time.Duration(index+1)*time.Nanosecond)); err != nil {
			t.Fatalf("StoreTmuxContext(%d) error = %v", index, err)
		}
	}

	cache, err := queue.loadTmuxCache()
	if err != nil {
		t.Fatalf("loadTmuxCache() error = %v", err)
	}
	if len(cache.Panes) != maxTmuxCacheEntries {
		t.Fatalf("cache entries = %d, want %d", len(cache.Panes), maxTmuxCacheEntries)
	}
	if _, ok := cache.Panes[tmuxCacheKey(expired)]; ok {
		t.Fatal("expected expired cache entry to be pruned")
	}
}

func TestStoreTmuxContextUsesCacheLock(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "state.json"))
	if err := ensureQueueDirs(queue); err != nil {
		t.Fatalf("ensureQueueDirs() error = %v", err)
	}
	lock, err := tryOpenQueueLock(queue.tmuxCacheLockPath())
	if err != nil {
		t.Fatalf("tryOpenQueueLock() error = %v", err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			t.Fatalf("closing cache lock: %v", err)
		}
	}()

	err = queue.StoreTmuxContext(context.Background(), testTmuxContext(), time.Now().UTC())
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("StoreTmuxContext() error = %v, want ErrLocked", err)
	}
}

func TestQueueOperationsHonorCanceledContext(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "state.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, enqueueErr := queue.Enqueue(ctx, testEnvelope("", "", "", time.Time{}), EnqueueOptions{Now: nil})
	if !errors.Is(enqueueErr, context.Canceled) {
		t.Fatalf("Enqueue() error = %v, want context.Canceled", enqueueErr)
	}
	storeErr := queue.StoreTmuxContext(ctx, testTmuxContext(), time.Now().UTC())
	if !errors.Is(storeErr, context.Canceled) {
		t.Fatalf("StoreTmuxContext() error = %v, want context.Canceled", storeErr)
	}
	_, statusErr := queue.Status(ctx)
	if !errors.Is(statusErr, context.Canceled) {
		t.Fatalf("Status() error = %v, want context.Canceled", statusErr)
	}
}

func testDrainOptions(now time.Time, processor Processor) DrainOptions {
	return DrainOptions{
		MaxItems:     0,
		LeaseTimeout: 0,
		Now:          func() time.Time { return now },
		Processor:    processor,
	}
}

func enqueueTestEnvelope(t *testing.T, queue Queue, envelope Envelope, now time.Time) EnqueueResult {
	t.Helper()
	result, err := queue.Enqueue(
		context.Background(),
		envelope,
		EnqueueOptions{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if result.ID == "" || result.Path == "" {
		t.Fatalf("expected enqueue id/path, got %#v", result)
	}

	return result
}

func requireQueueItemMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat queued item: %v", err)
	}
	if info.Mode().Perm() != fileMode {
		t.Fatalf("expected queue item mode %o, got %o", fileMode, info.Mode().Perm())
	}
}

func requireRetryMetadata(t *testing.T, path string) {
	t.Helper()
	retried, err := readEnvelope(path)
	if err != nil {
		t.Fatalf("reading retried envelope: %v", err)
	}
	if retried.Attempt != 1 || retried.LastError == "" || retried.NextAttemptAt.IsZero() {
		t.Fatalf("expected retry metadata, got %#v", retried)
	}
}

func requireQueueStatus(t *testing.T, queue Queue, pending int, processing int, dead int) {
	t.Helper()
	status, err := queue.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Pending != pending || status.Processing != processing || status.Dead != dead {
		t.Fatalf("unexpected queue status: got %#v, want pending=%d processing=%d dead=%d", status, pending, processing, dead)
	}
}

func requireNoQueueFiles(t *testing.T, dir string, label string) {
	t.Helper()
	if matches := globQueueFiles(t, dir); len(matches) != 0 {
		t.Fatalf("expected empty %s queue, got %#v", label, matches)
	}
}

func testEnvelope(storePath string, sessionID string, state registry.State, observedAt time.Time) Envelope {
	return Envelope{
		Version:             EnvelopeVersion,
		ID:                  NewEnvelopeID(observedAt),
		CreatedAt:           observedAt,
		StorePath:           storePath,
		Kind:                KindReport,
		Report:              ReportFromRegistry(testRegistryReport(sessionID, state, observedAt)),
		RawPayloadSet:       false,
		NoTmux:              false,
		Stdin:               nil,
		Runtime:             RuntimeContext{CWD: "", ParentArgs: nil, Env: nil},
		Attempt:             0,
		NextAttemptAt:       time.Time{},
		ProcessingStartedAt: time.Time{},
		WorkerPID:           0,
		LastError:           "",
		CachedTmux:          emptyTmuxContext(),
	}
}

func testRegistryReport(sessionID string, state registry.State, observedAt time.Time) registry.Report {
	return registry.Report{
		Harness:          registry.HarnessClaude,
		State:            state,
		SessionID:        sessionID,
		SessionPath:      "",
		ResumeCommand:    nil,
		CWD:              "/repo",
		ProjectRoot:      "",
		PID:              0,
		ProcessStartTime: "",
		PPID:             0,
		TTY:              "",
		Tmux:             emptyTmuxContext(),
		Source:           "test",
		Confidence:       "hook",
		Event:            "",
		Attributes:       nil,
		RawPayload:       nil,
		ObservedAt:       observedAt,
	}
}

func testTmuxContext() registry.TmuxContext {
	return registry.TmuxContext{
		Inside:          true,
		ServerSocket:    "/tmp/tmux",
		SessionID:       "",
		SessionName:     "work",
		WindowID:        "",
		WindowIndex:     "",
		WindowName:      "",
		PaneID:          "%4",
		PaneIndex:       "",
		PaneCurrentPath: "",
		PanePID:         0,
		PaneTTY:         "",
		ClientTTY:       "",
	}
}

func globQueueFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob queue files: %v", err)
	}

	return matches
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
