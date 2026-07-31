package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const expectedSessionSchemaVersion = 2

func TestJSONWritersPreservePrettyDocumentsAndSingleLineStreams(t *testing.T) {
	t.Parallel()
	value := map[string]any{"outer": map[string]bool{"value": true}}

	var stdout bytes.Buffer
	app := &application{stdout: &stdout}
	if err := app.writeJSON(value); err != nil {
		t.Fatal(err)
	}
	wantDocument := "{\n  \"outer\": {\n    \"value\": true\n  }\n}\n"
	if stdout.String() != wantDocument {
		t.Fatalf("formatted JSON = %q, want %q", stdout.String(), wantDocument)
	}

	stdout.Reset()
	if err := app.writeJSONLine(value); err != nil {
		t.Fatal(err)
	}
	wantLine := "{\"outer\":{\"value\":true}}\n"
	if stdout.String() != wantLine {
		t.Fatalf("JSON line = %q, want %q", stdout.String(), wantLine)
	}
}

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
	if len(prepared.observation.RawPayload) == 0 {
		t.Fatal("raw payload was not preserved")
	}
}

func TestPrepareReportIncludesNativeMultiplexerContext(t *testing.T) {
	t.Parallel()
	location := registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "work", PaneID: "terminal_7"}
	prepared, err := (&application{}).prepareReport(nil, reportOptions{
		harness: "codex", sessionID: "session", event: "turn_complete",
	}, reportRuntimeContext{multiplexer: location, defaultObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Multiplexer == nil || *prepared.observation.Multiplexer != location {
		t.Fatalf("multiplexer context = %#v", prepared.observation.Multiplexer)
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

func TestPrepareReportInfersLifecycleFromNativeEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		options     reportOptions
		payload     string
		lifecycle   registry.NativeLifecycle
		presence    registry.Presence
		nativeEvent string
	}{
		{
			name: "codex startup",
			options: reportOptions{
				harness: "codex", activity: "idle", rawDefaultsOnly: true,
			},
			payload:     `{"session_id":"codex-start","cwd":"/work","hook_event_name":"SessionStart","source":"startup","model":"gpt-5"}`,
			lifecycle:   registry.NativeLifecycleStart,
			presence:    registry.PresenceLive,
			nativeEvent: "SessionStart",
		},
		{
			name: "codex resume",
			options: reportOptions{
				harness: "codex", activity: "idle", rawDefaultsOnly: true,
			},
			payload:     `{"session_id":"codex-resume","cwd":"/work","hook_event_name":"SessionStart","source":"resume","model":"gpt-5"}`,
			lifecycle:   registry.NativeLifecycleResume,
			presence:    registry.PresenceLive,
			nativeEvent: "SessionStart",
		},
		{
			name: "codex end",
			options: reportOptions{
				harness: "codex", rawDefaultsOnly: true,
			},
			payload:     `{"session_id":"codex-end","cwd":"/work","hook_event_name":"SessionEnd","reason":"other","model":"gpt-5"}`,
			lifecycle:   registry.NativeLifecycleEnd,
			presence:    registry.PresenceGone,
			nativeEvent: "SessionEnd",
		},
		{
			name: "pi resume",
			options: reportOptions{
				harness: "pi", activity: "idle", event: "session_start", sessionPath: "/tmp/pi-session.json",
				attributes: []string{"pi_reason=resume"},
			},
			lifecycle:   registry.NativeLifecycleResume,
			presence:    registry.PresenceLive,
			nativeEvent: "session_start",
		},
		{
			name: "opencode deleted",
			options: reportOptions{
				harness: "opencode", event: "session.deleted", sessionID: "open-session",
			},
			lifecycle:   registry.NativeLifecycleEnd,
			presence:    registry.PresenceGone,
			nativeEvent: "session.deleted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prepared, err := (&application{}).prepareReport(
				strings.NewReader(test.payload),
				test.options,
				reportRuntimeContext{defaultObservedAt: time.Now().UTC()},
			)
			if err != nil {
				t.Fatal(err)
			}
			observation := prepared.observation
			if observation.Lifecycle == nil || *observation.Lifecycle != test.lifecycle {
				t.Fatalf("lifecycle = %#v, want %q", observation.Lifecycle, test.lifecycle)
			}
			if observation.Presence == nil || *observation.Presence != test.presence {
				t.Fatalf("presence = %#v, want %q", observation.Presence, test.presence)
			}
			if observation.NativeEvent != test.nativeEvent {
				t.Fatalf("native event = %q, want %q", observation.NativeEvent, test.nativeEvent)
			}
		})
	}
}

func TestInferredNativeEndCannotBeResurrectedByProcessEvidence(t *testing.T) {
	t.Parallel()

	store := registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	app := &application{}
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	process := &registry.ProcessIdentity{PID: 42, StartIdentity: "boot:42"}

	start, err := app.prepareReport(
		strings.NewReader(`{"session_id":"codex-session","cwd":"/work","hook_event_name":"SessionStart","source":"startup","model":"gpt-5"}`),
		reportOptions{harness: "codex", activity: "idle", rawDefaultsOnly: true},
		reportRuntimeContext{defaultObservedAt: at},
	)
	if err != nil {
		t.Fatal(err)
	}
	start.observation.Process = process
	session, err := store.Observe(context.Background(), start.observation)
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != registry.PresenceLive {
		t.Fatalf("start presence = %q, want live", session.Presence)
	}

	end, err := app.prepareReport(
		strings.NewReader(`{"session_id":"codex-session","cwd":"/work","hook_event_name":"SessionEnd","reason":"other","model":"gpt-5"}`),
		reportOptions{harness: "codex", rawDefaultsOnly: true},
		reportRuntimeContext{defaultObservedAt: at.Add(time.Second)},
	)
	if err != nil {
		t.Fatal(err)
	}
	end.observation.Process = process
	session, err = store.Observe(context.Background(), end.observation)
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != registry.PresenceGone || session.Activity != nil {
		t.Fatalf("end state = %#v", session)
	}

	present := true
	session, err = store.Observe(context.Background(), registry.Observation{
		Source: registry.ObservationSourceProcess, Evidence: registry.ObservationEvidenceProcessPresence,
		Harness: registry.HarnessCodex, Identity: registry.ObservationIdentity{SessionID: "codex-session"},
		ProcessPresent: &present, Process: process, ObservedAt: at.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Presence != registry.PresenceGone {
		t.Fatalf("process evidence resurrected ended session: %#v", session)
	}
}

func TestPrepareReportAddsNativeResumeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options reportOptions
		want    []string
	}{
		{
			name:    "codex id",
			options: reportOptions{harness: "codex", event: "turn", sessionID: "codex-session"},
			want:    []string{"codex", "resume", "codex-session"},
		},
		{
			name:    "pi path",
			options: reportOptions{harness: "pi", event: "agent_settled", sessionPath: "/tmp/pi-session.json"},
			want:    []string{"pi", "--session", "/tmp/pi-session.json"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prepared, err := (&application{}).prepareReport(nil, test.options, reportRuntimeContext{defaultObservedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			if prepared.observation.Catalog == nil || !slices.Equal(prepared.observation.Catalog.ResumeCommand, test.want) {
				t.Fatalf("resume command = %#v, want %#v", prepared.observation.Catalog, test.want)
			}
		})
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

func TestPrepareReportProcessEvidenceDoesNotCarryNativeAuthority(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{PID: 42, PPID: 10, ProcessGroupID: 42, StartIdentity: "boot:42", Executable: "/usr/bin/codex", CWD: "/work", TTY: "/dev/pts/4"}
	prepared, err := (&application{}).prepareReport(nil, reportOptions{
		harness: "codex", presence: "live", evidence: "process", pid: process.PID, event: "process.start",
	}, reportRuntimeContext{processes: []processinfo.Process{process}, defaultObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	observation := prepared.observation
	if observation.Source != registry.ObservationSourceProcess || observation.ActivityAuthoritative != nil || observation.Activity != nil || observation.NativeEvent != "" {
		t.Fatalf("process observation retained native fields: %#v", observation)
	}
	if err := registry.ValidateObservation(observation); err != nil {
		t.Fatalf("process observation is invalid: %v", err)
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

func TestPrepareReportAcceptsLargeCodexPostToolUseDefaults(t *testing.T) {
	t.Parallel()

	payload := `{"session_id":"codex-image","transcript_path":null,"cwd":"/work","hook_event_name":"PostToolUse","model":"gpt-5","tool_name":"view_image","tool_response":"` +
		strings.Repeat("x", maxPayloadInputBytes) + `"}`
	prepared, err := (&application{}).prepareReport(
		strings.NewReader(payload),
		reportOptions{harness: "codex", activity: "running", rawDefaultsOnly: true},
		reportRuntimeContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.observation.Identity.SessionID != "codex-image" || prepared.observation.NativeEvent != "PostToolUse" {
		t.Fatalf("PostToolUse metadata = %#v", prepared.observation)
	}
	if len(prepared.observation.RawPayload) != 0 {
		t.Fatalf("PostToolUse raw payload was retained: %d bytes", len(prepared.observation.RawPayload))
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

func TestReportHumanOutputDistinguishesReportedAndEffectiveActivity(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "report", "codex", "--session-id", "activity", "--event", "turn_complete", "--activity", "waiting", "--no-tmux"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, expected := range []string{"REPORTED", "EFFECTIVE", "AUTHORITATIVE", "waiting", "unknown", "no"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("report output omitted %q: %s", expected, output)
		}
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

func TestShowCommandUsesHumanOutputUnlessJSONRequested(t *testing.T) {
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
	root.SetArgs([]string{"--store", path, "show", session.ID})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "Session ID:") || strings.HasPrefix(strings.TrimSpace(human.String()), "{") {
		t.Fatalf("expected human session details, got %q", human.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "show", session.ID})
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
