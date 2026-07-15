package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const expectedSessionSchemaVersion = 2

func TestPrepareReportCarriesIndependentDimensions(t *testing.T) {
	t.Parallel()
	app := &application{}
	prepared, err := app.prepareReport(strings.NewReader(`{"session_id":"session-1","cwd":"/work","hook_event_name":"PermissionRequest","model":"gpt-5"}`), reportOptions{
		harness: "codex", presence: "live", activity: "waiting", sessionID: "session-1", event: "permission_prompt",
		cwd: "/work", projectRoot: "/work", resumeCommand: []string{"codex", "resume", "session-1"}, rawStdin: true,
	}, reportRuntimeContext{defaultObservedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Presence == nil || *prepared.observation.Presence != registry.PresenceLive || prepared.observation.Activity == nil || *prepared.observation.Activity != registry.ActivityWaiting {
		t.Fatalf("independent dimensions lost: %#v", prepared.observation)
	}
	if prepared.observation.Catalog == nil || len(prepared.observation.Catalog.ResumeCommand) != 3 {
		t.Fatalf("catalog metadata missing: %#v", prepared.observation.Catalog)
	}
	if string(prepared.stdin) == "" {
		t.Fatal("raw stdin was not preserved")
	}
}

func TestPrepareReportProcessEvidenceRequiresCompleteIdentity(t *testing.T) {
	t.Parallel()
	app := &application{}
	_, err := app.prepareReport(bytes.NewReader(nil), reportOptions{harness: "codex", evidence: "process", sessionID: "session-1", pid: 12}, reportRuntimeContext{})
	if err == nil {
		t.Fatal("expected incomplete process identity error")
	}
}

func TestPrepareReportRejectsConflictingStdinModes(t *testing.T) {
	t.Parallel()
	app := &application{}
	_, err := app.prepareReport(strings.NewReader(`{}`), reportOptions{harness: "codex", rawStdin: true, rawDefaultsOnly: true}, reportRuntimeContext{})
	if !errors.Is(err, errConflictingReportStdin) {
		t.Fatalf("stdin mode error = %v", err)
	}
}

func TestReportQuietSuppressesHumanOutput(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"--store", t.TempDir() + "/sessions.json", "report", "codex", "--session-id", "json", "--event", "start", "--quiet"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("quiet report wrote output: %q", stdout.String())
	}
}

func TestReportCommandDefaultsToHumanOutput(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "report", "codex", "--session-id", "human", "--event", "start", "--no-tmux"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "codex") {
		t.Fatalf("report default output = %q", stdout.String())
	}
}

func TestReportCommandEmitsJSONOnlyWhenRequested(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "--json", "report", "codex", "--session-id", "machine", "--event", "start", "--no-tmux"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var session registry.Session
	if err := json.Unmarshal(stdout.Bytes(), &session); err != nil || session.SchemaVersion != expectedSessionSchemaVersion {
		t.Fatalf("report JSON = %q, %v", stdout.String(), err)
	}
}

func TestReportJSONCoversIgnoredAndQueuedResults(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		args   []string
		stdin  string
		status string
	}{
		{name: "ignored", args: []string{"report", "claude", "--raw-stdin-defaults-only", "--no-tmux"}, stdin: `{"session_id":"codex-session","transcript_path":"/home/user/.codex/sessions/rollout.jsonl","hook_event_name":"Stop","model":"gpt-5-codex"}`, status: "ignored"},
		{name: "queued", args: []string{"report", "codex", "--session-id", "queued", "--event", "start", "--queue", "--no-tmux"}, status: "queued"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			root := NewRootCommand(&stdout, &bytes.Buffer{})
			root.SetIn(strings.NewReader(test.stdin))
			args := append([]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "--json"}, test.args...)
			root.SetArgs(args)
			if err := root.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			var result map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["status"] != test.status {
				t.Fatalf("result = %q, decoded=%#v, err=%v", stdout.String(), result, err)
			}
		})
	}
}

func TestGetCommandUsesHumanOutputUnlessJSONRequested(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sessions.json"
	store := registry.NewFileStore(path)
	at := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	activity := registry.ActivityIdle
	session, err := store.Observe(context.Background(), registry.Observation{
		Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent,
		Harness: registry.HarnessCodex, Identity: registry.ObservationIdentity{SessionID: "session-1"},
		NativeEvent: "Stop", Activity: &activity, ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	var human bytes.Buffer
	root := NewRootCommand(&human, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "get", session.ID})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "Session ID:") || strings.HasPrefix(strings.TrimSpace(human.String()), "{") {
		t.Fatalf("expected human session details, got %q", human.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "get", session.ID})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var decoded registry.Session
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
		t.Fatalf("expected JSON session: %v; output=%q", err, machine.String())
	}
	if decoded.ID != session.ID {
		t.Fatalf("session id = %q, want %q", decoded.ID, session.ID)
	}
}

func TestVersionHonorsJSONFlag(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--json", "--version"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON version: %v; output=%q", err, stdout.String())
	}
	if result["version"] == "" {
		t.Fatalf("missing version in %#v", result)
	}
}

func TestVersionDefaultsToHumanOutput(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--version"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.HasPrefix(stdout.String(), "agent-sessions ") {
		t.Fatalf("version default output = %q", stdout.String())
	}
}
