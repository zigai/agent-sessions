package registry

import (
	"errors"
	"fmt"
	rand "math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestStoreBackendsRemainEquivalentAcrossOperationSequences(t *testing.T) {
	t.Parallel()

	seeds := uint64(8)
	steps := 80
	if testing.Short() {
		seeds = 4
		steps = 40
	} else if os.Getenv("AHT_EXTENDED_PARITY") != "" {
		seeds = 32
		steps = 200
	}

	for seed := range seeds {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			t.Parallel()
			exerciseEquivalentStores(t, seed, steps)
		})
	}
}

func exerciseEquivalentStores(t *testing.T, seed uint64, steps int) {
	t.Helper()

	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	filePath := filepath.Join(t.TempDir(), "file.json")
	fileStore := NewFileStore(filePath)
	memoryStore, err := OpenMemoryStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatal(err)
	}
	fileStore.setNowForTest(func() time.Time { return base.Add(time.Hour) })
	memoryStore.setNowForTest(func() time.Time { return base.Add(time.Hour) })

	generator := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // Reproducible property-test sequences are required.
	sequences := map[string]uint64{}
	for step := range steps {
		// Semantic variation: inject conflicting batch periodically
		if step%25 == 15 {
			running, idle := ActivityRunning, ActivityIdle
			conflictBatch := []Observation{
				{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "conflict-item"}, Activity: &running, ObservedAt: base.Add(time.Duration(step) * time.Millisecond)},
				{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "conflict-item"}, Activity: &idle, ObservedAt: base.Add(time.Duration(step) * time.Millisecond)},
			}
			_, fileBatchErr := fileStore.ObserveBatch(t.Context(), conflictBatch)
			_, memBatchErr := memoryStore.ObserveBatch(t.Context(), conflictBatch)
			if !sameStoreError(fileBatchErr, memBatchErr) {
				t.Fatalf("step %d conflict batch: file %v, mem %v", step, fileBatchErr, memBatchErr)
			}
		}

		observation := generatedObservation(generator, sequences, base, step)
		fileSession, fileErr := fileStore.Observe(t.Context(), observation)
		memorySession, memoryErr := memoryStore.Observe(t.Context(), observation)
		if !sameStoreError(fileErr, memoryErr) {
			t.Fatalf("step %d observation %#v: file error %v, memory error %v", step, observation, fileErr, memoryErr)
		}
		if fileErr == nil {
			assertEquivalent(t, fmt.Sprintf("step %d session", step), fileSession, memorySession)
		}

		fileSessions, fileStateErr := fileStore.List(t.Context(), Filter{})
		memorySessions, memoryStateErr := memoryStore.List(t.Context(), Filter{})
		if fileStateErr != nil || memoryStateErr != nil {
			t.Fatalf("step %d list errors: file %v, memory %v", step, fileStateErr, memoryStateErr)
		}
		assertEquivalent(t, fmt.Sprintf("step %d sessions", step), fileSessions, memorySessions)

		// Semantic variation: reopen FileStore mid-sequence to verify persistence
		if step == steps/2 {
			reopenedFileStore := NewFileStore(filePath)
			reopenedFileStore.setNowForTest(func() time.Time { return base.Add(time.Hour) })
			reopenedSessions, err := reopenedFileStore.List(t.Context(), Filter{})
			if err != nil {
				t.Fatalf("reopened file store list error: %v", err)
			}
			assertEquivalent(t, fmt.Sprintf("step %d reopened sessions", step), reopenedSessions, memorySessions)
		}
	}
}

//nolint:cyclop // property generator exercises diverse observation variants
func generatedObservation(generator *rand.Rand, sequences map[string]uint64, base time.Time, step int) Observation {
	sessionID := fmt.Sprintf("session-%d", generator.IntN(8))
	activity := []Activity{ActivityIdle, ActivityRunning, ActivityWaiting}[generator.IntN(3)]
	harness := []Harness{HarnessClaude, HarnessCodex, HarnessOmp, HarnessPi}[generator.IntN(4)]
	observedAt := base.Add(time.Duration(step) * time.Millisecond)
	pid := 100 + generator.IntN(8)
	process := &ProcessIdentity{
		PID:            pid,
		PPID:           1,
		ProcessGroupID: pid,
		Foreground:     true,
		StartIdentity:  fmt.Sprintf("boot:%d", pid),
		Executable:     "/bin/agent",
	}

	// Semantic variation across observation sources and evidence types
	switch generator.IntN(6) {
	case 0:
		// Process presence evidence
		present := generator.IntN(2) == 0
		return Observation{
			Source:         ObservationSourceProcess,
			Evidence:       ObservationEvidenceProcessPresence,
			Harness:        harness,
			Identity:       ObservationIdentity{SessionID: sessionID},
			ProcessPresent: &present,
			Process:        process,
			ObservedAt:     observedAt,
		}
	case 1:
		// Screen state evidence
		return Observation{
			Source:   ObservationSourceScreen,
			Evidence: ObservationEvidenceScreenState,
			Harness:  harness,
			Identity: ObservationIdentity{SessionID: sessionID},
			Activity: &activity,
			Process:  process,
			Screen: &ScreenObservation{
				Activity:   activity,
				Authority:  "screen",
				Reason:     "screen_rule",
				Process:    *process,
				ObservedAt: observedAt,
			},
			ObservedAt: observedAt,
		}
	default:
		// Native evidence with varied sequence and lifecycle
		var seq *uint64
		switch generator.IntN(4) {
		case 0:
			// Replay current sequence
			if s, ok := sequences[sessionID]; ok && s > 0 {
				seq = &s
			}
		case 1:
			// Stale sequence
			if s, ok := sequences[sessionID]; ok && s > 1 {
				stale := s - 1
				seq = &stale
			}
		}
		if seq == nil {
			s := sequences[sessionID] + 1
			sequences[sessionID] = s
			seq = &s
		}

		var lifecycle *NativeLifecycle
		switch generator.IntN(5) {
		case 0:
			l := NativeLifecycleStart
			lifecycle = &l
		case 1:
			l := NativeLifecycleResume
			lifecycle = &l
		case 2:
			l := NativeLifecycleEnd
			lifecycle = &l
		}

		return Observation{
			Source:      ObservationSourceNative,
			Evidence:    ObservationEvidenceNativeEvent,
			Harness:     harness,
			Identity:    ObservationIdentity{SessionID: sessionID},
			Activity:    &activity,
			Lifecycle:   lifecycle,
			Sequence:    seq,
			Process:     process,
			NativeEvent: []string{"session_start", "agent_start", "agent_end", "permission_request"}[generator.IntN(4)],
			Attributes:  map[string]string{"aht_integration": "parity-test"},
			ObservedAt:  observedAt,
		}
	}
}

func sameStoreError(left error, right error) bool {
	for _, target := range []error{ErrSessionNotFound, ErrHarnessRequired, ErrObservationIdentity, ErrObservationConflict, ErrCorruptStore} {
		if errors.Is(left, target) != errors.Is(right, target) {
			return false
		}
	}
	return (left == nil) == (right == nil)
}

func assertEquivalent(t *testing.T, label string, left any, right any) {
	t.Helper()

	opt := cmp.Comparer(func(x, y ProcessIdentity) bool {
		return x == y
	})
	if diff := cmp.Diff(left, right, opt); diff != "" {
		t.Fatalf("%s mismatch (-file +memory):\n%s", label, diff)
	}
}
