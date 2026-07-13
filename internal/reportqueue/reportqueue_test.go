package reportqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	envelope := testEnvelope(storePath, "abc", registry.ActivityRunning, now)

	result := enqueueTestEnvelope(t, queue, envelope, now)
	requireQueueItemMode(t, result.Path)

	var processed []Envelope
	drain, err := queue.Drain(context.Background(), testDrainOptions(
		now.Add(time.Second),
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
	if processed[0].Report.Identity.SessionID != "abc" || !processed[0].Report.ObservedAt.Equal(now) {
		t.Fatalf("processed envelope mismatch: %#v", processed[0])
	}
	requireNoQueueFiles(t, queue.pendingDir(), "pending")
	requireNoQueueFiles(t, queue.processingDir(), "processing")
}

func TestQueueDrainRescansPendingBeforeUnlock(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	queue := New(storePath)
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "first", registry.ActivityRunning, now), now)

	var processed []string
	enqueuedSecond := false
	drain, err := queue.Drain(context.Background(), testDrainOptions(
		now.Add(time.Second),
		func(ctx context.Context, got Envelope) error {
			processed = append(processed, got.Report.Identity.SessionID)
			if got.Report.Identity.SessionID != "first" || enqueuedSecond {
				return nil
			}
			enqueuedSecond = true
			_, err := queue.Enqueue(
				ctx,
				testEnvelope(storePath, "second", registry.ActivityWaiting, now.Add(time.Nanosecond)),
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
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "abc", registry.ActivityRunning, now), now)

	drain, err := queue.Drain(context.Background(), testDrainOptions(
		now.Add(time.Second),
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
	enqueueTestEnvelope(t, queue, testEnvelope(storePath, "abc", registry.ActivityRunning, now), now)

	drain, err := queue.Drain(context.Background(), testDrainOptions(
		now.Add(time.Second),
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
	envelope := testEnvelope(storePath, "abc", registry.ActivityRunning, now)
	envelope.ID = "stale"
	envelope.ProcessingStartedAt = now.Add(-time.Hour)
	envelope.WorkerPID = 99999999
	processingPath := filepath.Join(queue.processingDir(), envelope.ID+".json")
	if err := writeJSONAtomic(processingPath, envelope); err != nil {
		t.Fatalf("writing processing envelope: %v", err)
	}

	options := testDrainOptions(
		now,
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

func testEnvelope(storePath string, sessionID string, activity registry.Activity, observedAt time.Time) Envelope {
	return Envelope{
		Version:             EnvelopeVersion,
		ID:                  NewEnvelopeID(observedAt),
		CreatedAt:           observedAt,
		StorePath:           storePath,
		Kind:                KindReport,
		Report:              ReportFromRegistry(testRegistryObservation(sessionID, activity, observedAt)),
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

func testRegistryObservation(sessionID string, activity registry.Activity, observedAt time.Time) registry.Observation {
	return registry.Observation{
		Source:      registry.ObservationSourceNative,
		Evidence:    registry.ObservationEvidenceNativeEvent,
		Harness:     registry.HarnessClaude,
		Identity:    registry.ObservationIdentity{SessionID: sessionID},
		Activity:    &activity,
		NativeEvent: "test",
		Attributes:  nil,
		RawPayload:  nil,
		ObservedAt:  observedAt,
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

func TestEnvelopeV2ObservationRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 27, 16, 30, 0, 0, time.UTC)
	observation := testRegistryObservation("session", registry.ActivityWaiting, now)
	observation.RawPayload = json.RawMessage("null")
	envelope := Envelope{
		Version:       EnvelopeVersion,
		ID:            "v2",
		CreatedAt:     now,
		StorePath:     "/tmp/store.json",
		Kind:          KindReport,
		Report:        ReportFromRegistry(observation),
		RawPayloadSet: true,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if decoded.Version != EnvelopeVersion {
		t.Fatalf("version = %d, want %d", decoded.Version, EnvelopeVersion)
	}
	if decoded.Report.Source != registry.ObservationSourceNative ||
		decoded.Report.Evidence != registry.ObservationEvidenceNativeEvent ||
		decoded.Report.Identity.SessionID != "session" ||
		string(decoded.Report.RawPayload) != "null" {
		t.Fatalf("decoded v2 observation = %#v", decoded.Report)
	}
	if decoded.Report.Activity == nil || *decoded.Report.Activity != registry.ActivityWaiting {
		t.Fatalf("decoded activity = %#v", decoded.Report.Activity)
	}
	if strings.Contains(string(encoded), `"state"`) {
		t.Fatalf("v2 envelope contains removed state field: %s", encoded)
	}
}

func TestQueueV1EnvelopeDeadLettersInvalid(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "state.json"))
	if err := ensureQueueDirs(queue); err != nil {
		t.Fatalf("ensureQueueDirs() error: %v", err)
	}
	path := filepath.Join(queue.pendingDir(), "v1.json")
	v1 := []byte(`{"version":1,"id":"v1","kind":"report","report":{"state":"running","session_id":"legacy"}}`)
	if err := os.WriteFile(path, v1, fileMode); err != nil {
		t.Fatalf("write v1 envelope: %v", err)
	}
	called := false
	result, err := queue.Drain(context.Background(), testDrainOptions(time.Now().UTC(), func(context.Context, Envelope) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("Drain() error: %v", err)
	}
	if called || result.Dead != 1 {
		t.Fatalf("v1 envelope result = %#v, processor called=%v", result, called)
	}
	if len(globQueueFiles(t, queue.pendingDir())) != 0 {
		t.Fatal("v1 envelope remained pending")
	}
	invalid, err := filepath.Glob(filepath.Join(queue.deadDir(), "*.invalid"))
	if err != nil {
		t.Fatalf("glob invalid dead letters: %v", err)
	}
	if len(invalid) != 1 {
		t.Fatalf("invalid dead letters = %d, want 1", len(invalid))
	}
	status, err := queue.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if status.Dead != 1 || status.Invalid != 1 {
		t.Fatalf("status = %#v, want dead=1 invalid=1", status)
	}
}

//nolint:cyclop // status test builds all queue categories and asserts their aggregate contract
func TestQueueStatusReportsBacklog(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "state.json"))
	if err := ensureQueueDirs(queue); err != nil {
		t.Fatalf("ensureQueueDirs() error: %v", err)
	}
	now := time.Now().UTC()
	deferred := testEnvelope("", "deferred", registry.ActivityRunning, now)
	deferred.NextAttemptAt = now.Add(time.Minute)
	if err := writeJSONAtomic(filepath.Join(queue.pendingDir(), "deferred.json"), deferred); err != nil {
		t.Fatalf("write deferred envelope: %v", err)
	}
	stale := testEnvelope("", "stale", registry.ActivityRunning, now.Add(time.Second))
	stale.Attempt = 2
	stale.ProcessingStartedAt = now.Add(-defaultLeaseTimeout - time.Second)
	stale.WorkerPID = 99999999
	if err := writeJSONAtomic(filepath.Join(queue.processingDir(), "stale.json"), stale); err != nil {
		t.Fatalf("write stale envelope: %v", err)
	}
	if err := os.WriteFile(filepath.Join(queue.deadDir(), "legacy.invalid"), []byte("invalid\n"), fileMode); err != nil {
		t.Fatalf("write invalid dead letter: %v", err)
	}
	status, err := queue.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if status.Pending != 1 || status.Ready != 0 || status.Deferred != 1 ||
		status.Processing != 1 || status.StaleLeases != 1 || status.Retries != 1 ||
		status.Dead != 1 || status.Invalid != 1 {
		t.Fatalf("status = %#v", status)
	}
	if !status.NextRetryAt.Equal(now.Add(time.Minute)) || status.OldestBacklogAt.IsZero() {
		t.Fatalf("status timestamps = %#v", status)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
