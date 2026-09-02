package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreBackendsRemainEquivalentAcrossOperationSequences(t *testing.T) {
	t.Parallel()

	for seed := range uint64(32) {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			t.Parallel()
			exerciseEquivalentStores(t, seed)
		})
	}
}

func exerciseEquivalentStores(t *testing.T, seed uint64) {
	t.Helper()

	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fileStore := NewFileStore(filepath.Join(t.TempDir(), "file.json"))
	memoryStore, err := OpenMemoryStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatal(err)
	}
	fileStore.SetNowForTest(func() time.Time { return base.Add(time.Hour) })
	memoryStore.SetNowForTest(func() time.Time { return base.Add(time.Hour) })

	generator := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // Reproducible property-test sequences are required.
	sequences := map[string]uint64{}
	for step := range 200 {
		observation := generatedObservation(generator, sequences, base, step)
		fileSession, fileErr := fileStore.Observe(t.Context(), observation)
		memorySession, memoryErr := memoryStore.Observe(t.Context(), observation)
		if !sameStoreError(fileErr, memoryErr) {
			t.Fatalf("step %d observation %#v: file error %v, memory error %v", step, observation, fileErr, memoryErr)
		}
		if fileErr == nil {
			assertSameJSON(t, fmt.Sprintf("step %d session", step), fileSession, memorySession)
		}

		fileSessions, fileStateErr := fileStore.List(t.Context(), Filter{})
		memorySessions, memoryStateErr := memoryStore.List(t.Context(), Filter{})
		if fileStateErr != nil || memoryStateErr != nil {
			t.Fatalf("step %d list errors: file %v, memory %v", step, fileStateErr, memoryStateErr)
		}
		assertSameJSON(t, fmt.Sprintf("step %d sessions", step), fileSessions, memorySessions)
	}
}

func generatedObservation(generator *rand.Rand, sequences map[string]uint64, base time.Time, step int) Observation {
	sessionID := fmt.Sprintf("session-%d", generator.IntN(8))
	activity := []Activity{ActivityIdle, ActivityRunning, ActivityWaiting}[generator.IntN(3)]
	sequence := sequences[sessionID] + 1
	sequences[sessionID] = sequence

	return Observation{
		Source:      ObservationSourceNative,
		Evidence:    ObservationEvidenceNativeEvent,
		Harness:     []Harness{HarnessClaude, HarnessCodex, HarnessOmp, HarnessPi}[generator.IntN(4)],
		Identity:    ObservationIdentity{SessionID: sessionID},
		Activity:    &activity,
		Sequence:    &sequence,
		NativeEvent: []string{"session_start", "agent_start", "agent_end", "permission_request"}[generator.IntN(4)],
		Attributes:  map[string]string{"aht_integration": "parity-test"},
		ObservedAt:  base.Add(time.Duration(step) * time.Millisecond),
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

func assertSameJSON(t *testing.T, label string, left any, right any) {
	t.Helper()

	leftJSON, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("%s differs:\nfile:   %s\nmemory: %s", label, leftJSON, rightJSON)
	}
}
