package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/reportqueue"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestProcessQueuedObservation(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	app := &application{storePath: storePath}
	at := time.Now().UTC().Add(-time.Minute)
	activity := registry.ActivityWaiting
	process := &registry.ProcessIdentity{PID: 42, StartIdentity: "boot:42", TTY: "/dev/pts/4"}
	envelope := reportqueue.Envelope{
		Version:   reportqueue.EnvelopeVersion,
		CreatedAt: at,
		StorePath: storePath,
		Kind:      reportqueue.KindReport,
		Report: reportqueue.ReportFromRegistry(registry.Observation{
			Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent,
			Harness: registry.HarnessClaude, NativeEvent: "permission_prompt", Activity: &activity,
			Identity: registry.ObservationIdentity{SessionID: "queued"}, Process: process, ObservedAt: at,
		}),
		CachedTmux: registry.TmuxContext{Inside: true, SessionName: "dev", PaneID: "%4", PaneTTY: "/dev/pts/4"},
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
	if sessions[0].Process == nil || sessions[0].Process.PID != process.PID || sessions[0].Tmux.PaneID != "%4" {
		t.Fatalf("queued session lost process or tmux context: %#v", sessions[0])
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

func TestQueueStatusUsesHumanOutputUnlessJSONRequested(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "sessions.json")

	var human bytes.Buffer
	root := NewRootCommand(&human, &bytes.Buffer{})
	root.SetArgs([]string{"--store", storePath, queueStatusCommandName})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(human.String(), "pending=") || strings.HasPrefix(strings.TrimSpace(human.String()), "{") {
		t.Fatalf("expected human queue status, got %q", human.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", storePath, "--json", queueStatusCommandName})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status reportqueue.StatusResult
	if err := json.Unmarshal(machine.Bytes(), &status); err != nil {
		t.Fatalf("expected JSON queue status: %v; output=%q", err, machine.String())
	}
}
