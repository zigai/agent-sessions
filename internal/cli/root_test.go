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

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const expectedSessionSchemaVersion = 2

//nolint:cyclop // assertions independently verify each report dimension
func TestPrepareReportCarriesIndependentDimensions(t *testing.T) {
	t.Parallel()
	app := &application{}
	prepared, err := app.prepareReport(strings.NewReader(`{"session_id":"session-1","cwd":"/work","hook_event_name":"PermissionRequest","model":"gpt-5"}`), reportOptions{
		harness: "codex", presence: "live", activity: "waiting", sessionID: "session-1", event: "permission_prompt",
		cwd: "/work", projectRoot: "/work", resumeCommand: []string{"codex", "resume", "session-1"}, rawStdin: true,
	}, reportRuntimeContext{
		tmux:              registry.TmuxContext{Inside: true, SessionName: "dev", PaneID: "%4"},
		defaultObservedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Presence == nil || *prepared.observation.Presence != registry.PresenceLive || prepared.observation.Activity == nil || *prepared.observation.Activity != registry.ActivityWaiting {
		t.Fatalf("independent dimensions lost: %#v", prepared.observation)
	}
	if prepared.observation.ActivityAuthoritative == nil || *prepared.observation.ActivityAuthoritative {
		t.Fatalf("Codex hook activity must be stored as a non-authoritative hint: %#v", prepared.observation)
	}
	if prepared.observation.Catalog == nil || len(prepared.observation.Catalog.ResumeCommand) != 3 {
		t.Fatalf("catalog metadata missing: %#v", prepared.observation.Catalog)
	}
	if prepared.observation.Tmux == nil || prepared.observation.Tmux.SessionName != "dev" || prepared.observation.Tmux.PaneID != "%4" {
		t.Fatalf("tmux context missing: %#v", prepared.observation.Tmux)
	}
	if string(prepared.stdin) == "" {
		t.Fatal("raw stdin was not preserved")
	}
}

func TestPrepareReportCarriesNativeLifecycle(t *testing.T) {
	t.Parallel()

	prepared, err := (&application{}).prepareReport(nil, reportOptions{
		harness: "openclaw", lifecycle: "resume", presence: "live", activity: "idle",
		sessionID: "native-session", event: "session_start",
	}, reportRuntimeContext{defaultObservedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Lifecycle == nil || *prepared.observation.Lifecycle != registry.NativeLifecycleResume {
		t.Fatalf("native lifecycle missing: %#v", prepared.observation)
	}
}

func TestOpenClawLifecycleReportsDriveDocumentedStateTransitions(t *testing.T) {
	t.Parallel()

	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	app := &application{}
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name, lifecycle, presence, activity string
		wantPresence                        registry.Presence
		wantActivity                        *registry.Activity
	}{
		{name: "session_start", lifecycle: "start", presence: "live", activity: "idle", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityIdle)},
		{name: "before_agent_run", lifecycle: "", presence: "live", activity: "running", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityRunning)},
		{name: "agent_end", lifecycle: "", presence: "live", activity: "idle", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityIdle)},
		{name: "session_end", lifecycle: "end", presence: "gone", activity: "", wantPresence: registry.PresenceGone, wantActivity: nil},
	}
	for index, test := range tests {
		prepared, err := app.prepareReport(nil, reportOptions{
			harness: "openclaw", lifecycle: test.lifecycle, presence: test.presence, activity: test.activity,
			sessionID: "openclaw-session", event: test.name,
		}, reportRuntimeContext{defaultObservedAt: base.Add(time.Duration(index) * time.Second)})
		if err != nil {
			t.Fatalf("preparing %s report: %v", test.name, err)
		}
		session, err := store.Observe(context.Background(), prepared.observation)
		if err != nil {
			t.Fatalf("recording %s report: %v", test.name, err)
		}
		if session.Presence != test.wantPresence || !equalActivity(session.Activity, test.wantActivity) {
			t.Fatalf("%s state = presence %q activity %#v", test.name, session.Presence, session.Activity)
		}
	}
}

func TestHermesLifecycleReportsDriveDocumentedStateTransitions(t *testing.T) {
	t.Parallel()

	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	app := &application{}
	base := time.Date(2026, 7, 18, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name, lifecycle, presence, activity string
		wantPresence                        registry.Presence
		wantActivity                        *registry.Activity
	}{
		{name: "on_session_start", lifecycle: "start", presence: "live", activity: "idle", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityIdle)},
		{name: "pre_llm_call", lifecycle: "", presence: "live", activity: "running", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityRunning)},
		{name: "pre_approval_request", lifecycle: "", presence: "live", activity: "waiting", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityWaiting)},
		{name: "post_approval_response", lifecycle: "", presence: "live", activity: "running", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityRunning)},
		{name: "on_session_end", lifecycle: "", presence: "live", activity: "idle", wantPresence: registry.PresenceLive, wantActivity: activityPointer(registry.ActivityIdle)},
		{name: "on_session_finalize", lifecycle: "end", presence: "gone", activity: "", wantPresence: registry.PresenceGone, wantActivity: nil},
	}
	for index, test := range tests {
		prepared, err := app.prepareReport(nil, reportOptions{
			harness: "hermes", lifecycle: test.lifecycle, presence: test.presence, activity: test.activity,
			sessionID: "hermes-session", event: test.name,
		}, reportRuntimeContext{defaultObservedAt: base.Add(time.Duration(index) * time.Second)})
		if err != nil {
			t.Fatalf("preparing %s report: %v", test.name, err)
		}
		session, err := store.Observe(context.Background(), prepared.observation)
		if err != nil {
			t.Fatalf("recording %s report: %v", test.name, err)
		}
		if session.Presence != test.wantPresence || !equalActivity(session.Activity, test.wantActivity) {
			t.Fatalf("%s state = presence %q activity %#v", test.name, session.Presence, session.Activity)
		}
	}
}

func activityPointer(activity registry.Activity) *registry.Activity { return &activity }

func equalActivity(left, right *registry.Activity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}

func TestPrepareReportAttachesMatchingAgentProcess(t *testing.T) {
	t.Parallel()
	app := &application{}
	agent := processinfo.Process{
		PID:            42,
		PPID:           10,
		ProcessGroupID: 42,
		StartIdentity:  "boot:42",
		Executable:     "/usr/bin/node",
		CWD:            "/work",
		TTY:            "/dev/pts/4",
		Args:           []string{"pi"},
	}
	prepared, err := app.prepareReport(nil, reportOptions{
		harness: "pi", activity: "running", sessionPath: "/tmp/session.json",
	}, reportRuntimeContext{processes: []processinfo.Process{
		{PID: 50, PPID: 42, StartIdentity: "boot:50", Executable: "/bin/sh", Args: []string{"sh"}},
		agent,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Process == nil || prepared.observation.Process.PID != agent.PID || prepared.observation.Process.StartIdentity != agent.StartIdentity {
		t.Fatalf("report process identity = %#v, want agent process %#v", prepared.observation.Process, agent)
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

func TestShimProcessReportsInferIdentityAndTransitionState(t *testing.T) {
	t.Parallel()

	process := processinfo.Process{PID: 42, PPID: 10, ProcessGroupID: 42, StartIdentity: "boot:42", Executable: "/bin/sh", CWD: "/work", TTY: "/dev/pts/4"}
	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	var sessionID string
	for index, test := range []struct {
		name     string
		presence string
		present  bool
	}{
		{name: "start", presence: "live", present: true},
		{name: "exit", presence: "gone", present: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessionID = requireShimProcessTransition(
				t, store, process, test.name, test.presence, test.present,
				base.Add(time.Duration(index)*time.Second), sessionID,
			)
		})
	}
}

func requireShimProcessTransition(
	t *testing.T,
	store *registry.FileStore,
	process processinfo.Process,
	name, presence string,
	present bool,
	observedAt time.Time,
	previousSessionID string,
) string {
	t.Helper()
	app := &application{}
	prepared, err := app.prepareReport(nil, reportOptions{
		harness: "droid", presence: presence, evidence: "process", pid: process.PID, event: "process." + name,
	}, reportRuntimeContext{processes: []processinfo.Process{process}, defaultObservedAt: observedAt})
	if err != nil {
		t.Fatal(err)
	}
	observation := prepared.observation
	requireShimObservation(t, observation, process, present)
	session, err := store.Observe(context.Background(), observation)
	if err != nil {
		t.Fatal(err)
	}
	if previousSessionID != "" && session.ID != previousSessionID {
		t.Fatalf("shim transitions split sessions: start=%q next=%q", previousSessionID, session.ID)
	}
	wantPresence := registry.PresenceLive
	if !present {
		wantPresence = registry.PresenceGone
	}
	if session.Presence != wantPresence {
		t.Fatalf("session presence = %q, want %q", session.Presence, wantPresence)
	}
	return session.ID
}

func requireShimObservation(t *testing.T, observation registry.Observation, process processinfo.Process, present bool) {
	t.Helper()
	if observation.Source != registry.ObservationSourceProcess || observation.Process == nil || !observation.Process.Complete() || observation.Process.StartIdentity != process.StartIdentity {
		t.Fatalf("shim process identity = %#v", observation.Process)
	}
	if observation.ProcessPresent == nil || *observation.ProcessPresent != present {
		t.Fatalf("process presence = %#v, want %v", observation.ProcessPresent, present)
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

func TestPrepareReportRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	app := &application{}
	_, err := app.prepareReport(
		strings.NewReader(strings.Repeat("x", maxPayloadInputBytes+1)),
		reportOptions{harness: "codex", rawStdin: true},
		reportRuntimeContext{},
	)
	if !errors.Is(err, errPayloadInputTooLarge) {
		t.Fatalf("error = %v, want %v", err, errPayloadInputTooLarge)
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
