package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/internal/reportqueue"
	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestProcessQueuedObservation(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	app := &application{storePath: storePath}
	at := time.Now().UTC().Add(-time.Minute)
	activity := registry.ActivityWaiting
	envelope := reportqueue.Envelope{
		Version:   reportqueue.EnvelopeVersion,
		CreatedAt: at,
		StorePath: storePath,
		Kind:      reportqueue.KindReport,
		Report: reportqueue.ReportFromRegistry(registry.Observation{
			Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent,
			Harness: registry.HarnessClaude, NativeEvent: "permission_prompt", Activity: &activity,
			Identity: registry.ObservationIdentity{SessionID: "queued"}, ObservedAt: at,
		}),
	}
	if err := app.processQueuedReport(context.Background(), reportqueue.New(storePath), envelope); err != nil {
		t.Fatal(err)
	}
	sessions, err := app.store().List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Activity == nil || *sessions[0].Activity != registry.ActivityWaiting {
		t.Fatalf("unexpected queued session: %#v", sessions)
	}
}

func TestValidateQueuedObservationRejectsMissingIdentity(t *testing.T) {
	err := validateQueuedObservation(registry.Observation{Harness: registry.HarnessClaude})
	if err == nil {
		t.Fatal("expected missing identity error")
	}
}

func TestValidateQueuedEnvelopeRejectsLegacyVersion(t *testing.T) {
	err := validateQueuedEnvelope(reportqueue.Envelope{Version: 1, Kind: reportqueue.KindReport})
	if err == nil {
		t.Fatal("expected unsupported queue version")
	}
}
