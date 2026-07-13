package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

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

func TestReportCommandEmitsV2JSON(t *testing.T) {
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
