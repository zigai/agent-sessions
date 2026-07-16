package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func BenchmarkPrepareReport(b *testing.B) {
	app := &application{}
	options := reportOptions{harness: "codex", sessionID: "benchmark", event: "turn_start", activity: "running"}
	for b.Loop() {
		if _, err := app.prepareReport(nil, options, reportRuntimeContext{defaultObservedAt: time.Now().UTC()}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkObserveBatch(b *testing.B) {
	store := registry.NewFileStore(filepath.Join(b.TempDir(), "sessions.json"))
	observation := registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, NativeEvent: "turn_start", Harness: registry.HarnessCodex, Identity: registry.ObservationIdentity{SessionID: "benchmark"}, ObservedAt: time.Now().UTC()}
	b.ResetTimer()
	for b.Loop() {
		if _, err := store.Observe(context.Background(), observation); err != nil {
			b.Fatal(err)
		}
	}
}
