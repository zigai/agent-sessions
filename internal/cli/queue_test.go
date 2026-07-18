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

func TestProcessQueuedObservationRestoresZellijRuntimeContext(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	app := &application{storePath: storePath}
	at := time.Now().UTC().Add(-time.Minute)
	envelope := reportqueue.Envelope{
		Version: reportqueue.EnvelopeVersion, CreatedAt: at, StorePath: storePath, Kind: reportqueue.KindReport,
		Report: reportqueue.ReportFromRegistry(registry.Observation{
			Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent,
			Harness: registry.HarnessCodex, NativeEvent: "turn_complete",
			Identity: registry.ObservationIdentity{SessionID: "queued-zellij"}, ObservedAt: at,
		}),
		Runtime: reportqueue.RuntimeContext{Env: map[string]string{"ZELLIJ_SESSION_NAME": "work", "ZELLIJ_PANE_ID": "7"}},
	}
	if err := app.processQueuedReport(context.Background(), reportqueue.New(storePath), envelope); err != nil {
		t.Fatal(err)
	}
	sessions, err := app.store().List(context.Background(), registry.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Multiplexer.Kind != registry.MultiplexerZellij || sessions[0].Multiplexer.SessionName != "work" || sessions[0].Multiplexer.PaneID != "terminal_7" {
		t.Fatalf("queued zellij session = %#v", sessions)
	}
}

func TestQueuedReportEnvelopePreservesNoTmux(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	envelope := queuedReportEnvelope("/tmp/state.json", preparedReport{
		harness: registry.HarnessOpenClaw,
		observation: registry.Observation{
			Harness:  registry.HarnessOpenClaw,
			Identity: registry.ObservationIdentity{SessionID: "session"},
		},
	}, true, at, reportqueue.RuntimeContext{}, registry.TmuxContext{Inside: true, SessionName: "foreign"})
	if !envelope.NoTmux {
		t.Fatal("queued report lost --no-tmux")
	}
}

func TestQueuedDefaultsOnlyEnvelopeDoesNotPersistStdin(t *testing.T) {
	t.Parallel()

	const secret = "prompt text that must not be queued"
	prepared, err := (&application{}).prepareReport(strings.NewReader(`{"session_id":"session","cwd":"/tmp","hook_event_name":"Stop","model":"gpt-5","prompt":"`+secret+`"}`), reportOptions{
		harness: "codex", activity: "idle", rawDefaultsOnly: true,
	}, reportRuntimeContext{defaultObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	envelope := queuedReportEnvelope("/tmp/state.json", prepared, false, time.Now().UTC(), reportqueue.RuntimeContext{}, registry.TmuxContext{})
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("stdin_base64")) {
		t.Fatalf("defaults-only queue envelope retained stdin: %s", data)
	}
}

func TestStartQueueDrainerReportsSpawnFailure(t *testing.T) {
	t.Parallel()

	err := startQueueDrainer(context.Background(), filepath.Join(t.TempDir(), "missing-drainer"), nil)
	if err == nil {
		t.Fatal("expected queue drainer start error")
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
