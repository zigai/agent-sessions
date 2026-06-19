package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const testSessionID = "abc"

func TestResumeCommandFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		harness     registry.Harness
		sessionID   string
		sessionPath string
		want        []string
	}{
		{
			name:        "claude",
			harness:     registry.HarnessClaude,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{"claude", "--resume", testSessionID},
		},
		{
			name:        codexCommand,
			harness:     registry.HarnessCodex,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{codexCommand, "resume", testSessionID},
		},
		{
			name:        "cursor",
			harness:     registry.HarnessCursor,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{cursorCommand, "--resume", testSessionID},
		},
		{
			name:        kimiCommand,
			harness:     registry.HarnessKimiCode,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{kimiCommand, "--session", testSessionID},
		},
		{
			name:        "grok",
			harness:     registry.HarnessGrok,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{"grok", "--resume", testSessionID},
		},
		{
			name:        "pi path",
			harness:     registry.HarnessPi,
			sessionID:   testSessionID,
			sessionPath: "/tmp/session.jsonl",
			want:        []string{"pi", "--session", "/tmp/session.jsonl"},
		},
		{
			name:        "opencode",
			harness:     registry.HarnessOpenCode,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{"opencode", "--session", testSessionID},
		},
		{
			name:        "agy",
			harness:     registry.HarnessAgy,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{"agy", "--conversation", testSessionID},
		},
		{
			name:        kiloCommand,
			harness:     registry.HarnessKilo,
			sessionID:   testSessionID,
			sessionPath: "",
			want:        []string{kiloCommand, "--session", testSessionID},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ResumeCommandFor(test.harness, test.sessionID, test.sessionPath)
			if !slices.Equal(got, test.want) {
				t.Fatalf("expected %#v, got %#v", test.want, got)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  registry.Harness
	}{
		{name: codexCommand, value: codexCommand, want: registry.HarnessCodex},
		{name: "cursor alias binary", value: "cursor-agent", want: registry.HarnessCursor},
		{name: "cursor alias cli hyphen", value: "cursor-cli", want: registry.HarnessCursor},
		{name: "cursor alias cli underscore", value: "cursor_cli", want: registry.HarnessCursor},
		{name: "claude alias hyphen", value: "claude-code", want: registry.HarnessClaude},
		{name: "claude alias underscore", value: "claude_code", want: registry.HarnessClaude},
		{name: "kimi-code", value: "kimi-code", want: registry.HarnessKimiCode},
		{name: "kimi alias", value: "kimi", want: registry.HarnessKimiCode},
		{name: "kimi alias underscore", value: "kimi_code", want: registry.HarnessKimiCode},
		{name: "kimi alias compact", value: "kimicode", want: registry.HarnessKimiCode},
		{name: "grok alias hyphen", value: "grok-build", want: registry.HarnessGrok},
		{name: "grok alias underscore", value: "grok_build", want: registry.HarnessGrok},
		{name: "opencode alias hyphen", value: "open-code", want: registry.HarnessOpenCode},
		{name: "opencode alias underscore", value: "open_code", want: registry.HarnessOpenCode},
		{name: "agy alias", value: "antigravity-cli", want: registry.HarnessAgy},
		{name: "agy google alias", value: "google_antigravity", want: registry.HarnessAgy},
		{name: kiloCommand, value: kiloCommand, want: registry.HarnessKilo},
		{name: "kilo alias command", value: "kilocode", want: registry.HarnessKilo},
		{name: "kilo alias hyphen", value: "kilo-code", want: registry.HarnessKilo},
		{name: "kilo alias underscore", value: "kilo_code", want: registry.HarnessKilo},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := Normalize(test.value)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestSupportedNames(t *testing.T) {
	t.Parallel()

	want := []string{"claude", codexCommand, "cursor", "kimi-code", "grok", "pi", "opencode", "agy", kiloCommand}
	got := SupportedNames()
	if !slices.Equal(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestEnvNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field EnvField
		want  []string
	}{
		{
			name:  "session id",
			field: EnvSessionID,
			want: []string{
				"AGENT_SESSIONS_SESSION_ID",
				"AGENT_SESSION_ID",
				"CLAUDE_SESSION_ID",
				"CODEX_SESSION_ID",
				"GROK_SESSION_ID",
				"PI_SESSION_ID",
				"OPENCODE_SESSION_ID",
				"KILO_SESSION_ID",
				"KILOCODE_SESSION_ID",
			},
		},
		{
			name:  "event",
			field: EnvEvent,
			want: []string{
				"AGENT_SESSIONS_EVENT",
				"AGENT_EVENT",
				"GROK_HOOK_EVENT",
				"KILO_EVENT",
				"KILOCODE_EVENT",
			},
		},
		{
			name:  "session path",
			field: EnvSessionPath,
			want: []string{
				"AGENT_SESSIONS_SESSION_PATH",
				"AGENT_SESSION_PATH",
				"CLAUDE_SESSION_PATH",
				"CODEX_SESSION_PATH",
				"CURSOR_TRANSCRIPT_PATH",
				"PI_SESSION_PATH",
				"OPENCODE_SESSION_PATH",
				"KILO_SESSION_PATH",
				"KILOCODE_SESSION_PATH",
			},
		},
		{
			name:  "project root",
			field: EnvProjectRoot,
			want: []string{
				"AGENT_SESSIONS_PROJECT_ROOT",
				"PROJECT_ROOT",
				"CURSOR_PROJECT_DIR",
				"CLAUDE_PROJECT_DIR",
				"GROK_WORKSPACE_ROOT",
				"KILO_PROJECT_ROOT",
				"KILOCODE_PROJECT_ROOT",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := EnvNames(test.field)
			if !slices.Equal(got, test.want) {
				t.Fatalf("expected %#v, got %#v", test.want, got)
			}
		})
	}
}

func TestFromCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		command string
		want    registry.Harness
		wantOK  bool
	}{
		{command: "/usr/bin/codex", want: registry.HarnessCodex, wantOK: true},
		{command: "/usr/local/bin/cursor-agent", want: registry.HarnessCursor, wantOK: true},
		{command: "agent", want: "", wantOK: false},
		{command: "claude", want: registry.HarnessClaude, wantOK: true},
		{command: "kimi", want: registry.HarnessKimiCode, wantOK: true},
		{command: "grok", want: registry.HarnessGrok, wantOK: true},
		{command: "grok-build", want: registry.HarnessGrok, wantOK: true},
		{command: "pi", want: registry.HarnessPi, wantOK: true},
		{command: "opencode", want: registry.HarnessOpenCode, wantOK: true},
		{command: "agy", want: registry.HarnessAgy, wantOK: true},
		{command: kiloCommand, want: registry.HarnessKilo, wantOK: true},
		{command: "kilocode", want: registry.HarnessKilo, wantOK: true},
		{command: "kilo-code", want: registry.HarnessKilo, wantOK: true},
		{command: "kilo_code", want: registry.HarnessKilo, wantOK: true},
		{command: "zsh", want: "", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			t.Parallel()

			got, ok := FromCommand(test.command)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("expected (%q, %t), got (%q, %t)", test.want, test.wantOK, got, ok)
			}
		})
	}
}

func TestDefaultsFromPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		harness    registry.Harness
		payload    string
		wantID     string
		wantPath   string
		wantCWD    string
		wantRoot   string
		wantEvent  string
		wantAttr   string
		wantAttrKV string
	}{
		{
			name:       "claude",
			harness:    registry.HarnessClaude,
			payload:    `{"session_id":"claude-session","transcript_path":"/tmp/claude.jsonl","cwd":"/tmp","hook_event_name":"SessionStart","source":"startup","model":"claude-sonnet-4-6"}`,
			wantID:     "claude-session",
			wantPath:   "/tmp/claude.jsonl",
			wantCWD:    "/tmp",
			wantRoot:   "",
			wantEvent:  "SessionStart",
			wantAttr:   "claude_model",
			wantAttrKV: "claude-sonnet-4-6",
		},
		{
			name:       codexCommand,
			harness:    registry.HarnessCodex,
			payload:    `{"session_id":"codex-session","transcript_path":"/tmp/codex.jsonl","cwd":"/tmp","hook_event_name":"UserPromptSubmit","model":"gpt-5-codex"}`,
			wantID:     "codex-session",
			wantPath:   "/tmp/codex.jsonl",
			wantCWD:    "/tmp",
			wantRoot:   "",
			wantEvent:  "UserPromptSubmit",
			wantAttr:   "codex_model",
			wantAttrKV: "gpt-5-codex",
		},
		{
			name:       "grok",
			harness:    registry.HarnessGrok,
			payload:    `{"sessionId":"grok-session","cwd":"/tmp","workspaceRoot":"/repo","hookEventName":"UserPromptSubmit","toolName":"run_terminal_command"}`,
			wantID:     "grok-session",
			wantPath:   "",
			wantCWD:    "/tmp",
			wantRoot:   "/repo",
			wantEvent:  "UserPromptSubmit",
			wantAttr:   "grok_tool_name",
			wantAttrKV: "run_terminal_command",
		},
		{
			name:       "cursor",
			harness:    registry.HarnessCursor,
			payload:    `{"conversation_id":"cursor-conversation","session_id":"cursor-session","transcript_path":"/tmp/cursor.jsonl","workspace_roots":["/repo"],"hook_event_name":"beforeSubmitPrompt","model":"gpt-5.2","cursor_version":"1.7.2","composer_mode":"agent","is_background_agent":false}`,
			wantID:     "cursor-session",
			wantPath:   "/tmp/cursor.jsonl",
			wantCWD:    "/repo",
			wantRoot:   "/repo",
			wantEvent:  "beforeSubmitPrompt",
			wantAttr:   "cursor_model",
			wantAttrKV: "gpt-5.2",
		},
		{
			name:       kimiCommand,
			harness:    registry.HarnessKimiCode,
			payload:    `{"session_id":"kimi-payload-session-no-index","cwd":"/tmp","hook_event_name":"PermissionRequest","tool_name":"Bash","turn_id":7}`,
			wantID:     "kimi-payload-session-no-index",
			wantPath:   "",
			wantCWD:    "/tmp",
			wantRoot:   "",
			wantEvent:  "PermissionRequest",
			wantAttr:   "kimi_code_tool_name",
			wantAttrKV: "Bash",
		},
		{
			name:       "agy",
			harness:    registry.HarnessAgy,
			payload:    `{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"event":"PreToolUse","toolCall":{"name":"run_command","args":{"Cwd":"/repo/subdir"}}}`,
			wantID:     "agy-session",
			wantPath:   "/repo/.gemini/antigravity/transcript.jsonl",
			wantCWD:    "/repo/subdir",
			wantRoot:   "/repo",
			wantEvent:  "PreToolUse",
			wantAttr:   "agy_tool_name",
			wantAttrKV: "run_command",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := DefaultsFromPayload(test.harness, json.RawMessage(test.payload))
			if got.SessionID != test.wantID ||
				got.SessionPath != test.wantPath ||
				got.CWD != test.wantCWD ||
				got.ProjectRoot != test.wantRoot ||
				got.Event != test.wantEvent {
				t.Fatalf("unexpected defaults: %#v", got)
			}
			if got.Attributes[test.wantAttr] != test.wantAttrKV {
				t.Fatalf("expected attribute %s=%q, got %#v", test.wantAttr, test.wantAttrKV, got.Attributes)
			}
		})
	}
}

func TestKimiDefaultsFromPayloadUsesSessionIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	sessionDir := filepath.Join(home, "sessions", "wd_repo_abc", "kimi-index-session")
	index := `{"sessionId":"other","sessionDir":"/tmp/other","workDir":"/tmp"}` + "\n" +
		`{"sessionId":"kimi-index-session","sessionDir":` + strconvQuoteForTest(sessionDir) + `,"workDir":"/repo"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatalf("writing session index: %v", err)
	}

	got := DefaultsFromPayload(
		registry.HarnessKimiCode,
		json.RawMessage(`{"session_id":"kimi-index-session","cwd":"/repo","hook_event_name":"SessionStart","source":"startup"}`),
	)

	if got.SessionID != "kimi-index-session" ||
		got.SessionPath != sessionDir ||
		got.CWD != "/repo" ||
		got.Event != "SessionStart" {
		t.Fatalf("unexpected defaults: %#v", got)
	}
	if got.Attributes["kimi_code_start_source"] != "startup" {
		t.Fatalf("expected kimi_code_start_source=startup, got %#v", got.Attributes)
	}
}

func TestPayloadCompatibleWithHarness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		harness registry.Harness
		payload string
		want    bool
	}{
		{
			name:    "claude accepts native hook payload",
			harness: registry.HarnessClaude,
			payload: `{"session_id":"claude-session","transcript_path":"/home/zigai/.claude/projects/-repo/claude-session.jsonl","cwd":"/repo","hook_event_name":"Stop"}`,
			want:    true,
		},
		{
			name:    "codex accepts native hook payload",
			harness: registry.HarnessCodex,
			payload: `{"session_id":"codex-session","transcript_path":"/home/zigai/.codex/sessions/2026/06/18/rollout.jsonl","cwd":"/repo","hook_event_name":"Stop","model":"gpt-5-codex"}`,
			want:    true,
		},
		{
			name:    "codex accepts null transcript path",
			harness: registry.HarnessCodex,
			payload: `{"session_id":"codex-session","transcript_path":null,"cwd":"/repo","hook_event_name":"Stop","model":"gpt-5-codex"}`,
			want:    true,
		},
		{
			name:    "cursor accepts native hook payload",
			harness: registry.HarnessCursor,
			payload: `{"conversation_id":"cursor-conversation","session_id":"cursor-session","transcript_path":null,"workspace_roots":["/repo"],"hook_event_name":"sessionEnd","cursor_version":"2026.06.15"}`,
			want:    true,
		},
		{
			name:    "grok accepts native hook payload",
			harness: registry.HarnessGrok,
			payload: `{"hookEventName":"stop","sessionId":"grok-session","cwd":"/repo","workspaceRoot":"/repo"}`,
			want:    true,
		},
		{
			name:    "grok accepts snake case hook payload",
			harness: registry.HarnessGrok,
			payload: `{"hook_event_name":"stop","session_id":"grok-session","cwd":"/repo","workspace_root":"/repo"}`,
			want:    true,
		},
		{
			name:    "kimi-code accepts native hook payload",
			harness: registry.HarnessKimiCode,
			payload: `{"session_id":"kimi-session","cwd":"/repo","hook_event_name":"PermissionRequest"}`,
			want:    true,
		},
		{
			name:    "agy accepts native hook payload",
			harness: registry.HarnessAgy,
			payload: `{"conversationId":"agy-session","workspacePaths":["/repo"],"event":"Stop"}`,
			want:    true,
		},
		{
			name:    "agy accepts snake case hook payload",
			harness: registry.HarnessAgy,
			payload: `{"conversation_id":"agy-session","workspace_paths":["/repo"],"event":"Stop"}`,
			want:    true,
		},
		{
			name:    "claude rejects camel case hook payload",
			harness: registry.HarnessClaude,
			payload: `{"hookEventName":"stop","sessionId":"not-claude","cwd":"/repo","workspaceRoot":"/repo","promptId":"prompt"}`,
			want:    false,
		},
		{
			name:    "claude rejects cursor payload",
			harness: registry.HarnessClaude,
			payload: `{"conversation_id":"cursor-conversation","session_id":"cursor-session","transcript_path":null,"hook_event_name":"sessionEnd","cursor_version":"2026.06.15","workspace_roots":["/repo"]}`,
			want:    false,
		},
		{
			name:    "claude rejects codex transcript payload",
			harness: registry.HarnessClaude,
			payload: `{"session_id":"codex-session","transcript_path":"/home/zigai/.codex/sessions/2026/06/18/rollout.jsonl","cwd":"/repo","hook_event_name":"Stop","model":"gpt-5-codex"}`,
			want:    false,
		},
		{
			name:    "claude rejects null transcript path",
			harness: registry.HarnessClaude,
			payload: `{"session_id":"claude-session","transcript_path":null,"cwd":"/repo","hook_event_name":"Stop"}`,
			want:    false,
		},
		{
			name:    "codex rejects claude transcript payload",
			harness: registry.HarnessCodex,
			payload: `{"session_id":"claude-session","transcript_path":"/home/zigai/.claude/projects/-repo/claude-session.jsonl","cwd":"/repo","hook_event_name":"SessionStart","model":"claude-sonnet-4-6"}`,
			want:    false,
		},
		{
			name:    "cursor rejects payload without cursor common fields",
			harness: registry.HarnessCursor,
			payload: `{"session_id":"cursor-session","hook_event_name":"sessionStart"}`,
			want:    false,
		},
		{
			name:    "cursor rejects string workspace roots",
			harness: registry.HarnessCursor,
			payload: `{"session_id":"cursor-session","transcript_path":"/tmp/cursor.jsonl","workspace_roots":"/repo","hook_event_name":"sessionEnd","cursor_version":"2026.06.15"}`,
			want:    false,
		},
		{
			name:    "cursor rejects blank workspace roots",
			harness: registry.HarnessCursor,
			payload: `{"session_id":"cursor-session","transcript_path":"/tmp/cursor.jsonl","workspace_roots":["  "],"hook_event_name":"sessionEnd","cursor_version":"2026.06.15"}`,
			want:    false,
		},
		{
			name:    "claude rejects non-object json",
			harness: registry.HarnessClaude,
			payload: `"not an object"`,
			want:    false,
		},
		{
			name:    "claude rejects invalid json",
			harness: registry.HarnessClaude,
			payload: `{"session_id":`,
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := PayloadCompatibleWithHarness(test.harness, json.RawMessage(test.payload))
			if got != test.want {
				t.Fatalf("expected %t, got %t", test.want, got)
			}
		})
	}
}

type agyHookTestCase struct {
	name         string
	event        string
	payload      map[string]any
	parentArgs   []string
	wantReport   bool
	wantState    registry.State
	wantDecision string
	wantEmpty    bool
	wantHeadless bool
}

func TestHandleHookAgy(t *testing.T) {
	t.Parallel()

	tests := []agyHookTestCase{
		{
			name:  "pre invocation reports running",
			event: "PreInvocation",
			payload: map[string]any{
				"conversationId": "agy-session",
				"transcriptPath": "/repo/.gemini/antigravity/transcript.jsonl",
				"workspacePaths": []any{"/repo"},
				"invocationNum":  float64(3),
			},
			parentArgs:   nil,
			wantReport:   true,
			wantState:    registry.StateRunning,
			wantDecision: "",
			wantEmpty:    true,
			wantHeadless: false,
		},
		{
			name:  "pre tool use permission reports waiting",
			event: "PreToolUse",
			payload: map[string]any{
				"conversationId": "agy-session",
				"transcriptPath": "/repo/.gemini/antigravity/transcript.jsonl",
				"workspacePaths": []any{"/repo"},
				"toolCall": map[string]any{
					"name": "ask_permission",
					"args": map[string]any{"Cwd": "/repo/pkg"},
				},
			},
			parentArgs:   nil,
			wantReport:   true,
			wantState:    registry.StateWaiting,
			wantDecision: "allow",
			wantEmpty:    false,
			wantHeadless: false,
		},
		{
			name:  "fully idle stop reports idle",
			event: "Stop",
			payload: map[string]any{
				"conversationId":    "agy-session",
				"transcriptPath":    "/repo/.gemini/antigravity/transcript.jsonl",
				"workspacePaths":    []any{"/repo"},
				"terminationReason": "model_stop",
				"fullyIdle":         true,
			},
			parentArgs:   nil,
			wantReport:   true,
			wantState:    registry.StateIdle,
			wantDecision: "",
			wantEmpty:    false,
			wantHeadless: false,
		},
		{
			name:  "headless fully idle stop reports exited",
			event: "Stop",
			payload: map[string]any{
				"conversationId": "agy-session",
				"transcriptPath": "/repo/.gemini/antigravity/transcript.jsonl",
				"workspacePaths": []any{"/repo"},
				"fullyIdle":      true,
			},
			parentArgs:   []string{"agy", "--print", "hello"},
			wantReport:   true,
			wantState:    registry.StateExited,
			wantDecision: "",
			wantEmpty:    false,
			wantHeadless: true,
		},
		{
			name:  "empty post tool use does not report",
			event: "PostToolUse",
			payload: map[string]any{
				"conversationId": "agy-session",
				"transcriptPath": "/repo/.gemini/antigravity/transcript.jsonl",
				"workspacePaths": []any{"/repo"},
				"toolCall":       nil,
			},
			parentArgs:   nil,
			wantReport:   false,
			wantState:    "",
			wantDecision: "",
			wantEmpty:    true,
			wantHeadless: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertAgyHookResult(t, test)
		})
	}
}

func assertAgyHookResult(t *testing.T, test agyHookTestCase) {
	t.Helper()

	result, ok := HandleHook(
		registry.HarnessAgy,
		test.event,
		json.RawMessage(`{"test":true}`),
		test.payload,
		test.parentArgs,
	)
	if !ok {
		t.Fatal("expected agy hook adapter")
	}
	if result.ReportOK != test.wantReport {
		t.Fatalf("expected report ok %v, got %v", test.wantReport, result.ReportOK)
	}
	if result.ReportOK && result.Report.State != test.wantState {
		t.Fatalf("expected state %q, got %q", test.wantState, result.Report.State)
	}
	assertAgyHookResponse(t, result.Response, test)
	if test.wantHeadless && result.Report.Attributes["agy_headless"] != "true" {
		t.Fatalf("expected headless attribute, got %#v", result.Report.Attributes)
	}
}

func assertAgyHookResponse(t *testing.T, response map[string]any, test agyHookTestCase) {
	t.Helper()

	if test.wantEmpty {
		if len(response) != 0 {
			t.Fatalf("expected empty response, got %#v", response)
		}

		return
	}
	if response["decision"] != test.wantDecision {
		t.Fatalf("expected decision %q, got %#v", test.wantDecision, response)
	}
}

func TestHandleHookUnsupportedHarness(t *testing.T) {
	t.Parallel()

	var rawPayload json.RawMessage
	var payload map[string]any
	if _, ok := HandleHook(registry.HarnessCodex, "Stop", rawPayload, payload, nil); ok {
		t.Fatal("expected codex to have no managed hook adapter")
	}
}

func strconvQuoteForTest(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return string(data)
}
