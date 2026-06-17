package harness

import (
	"encoding/json"
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
		{name: "claude alias hyphen", value: "claude-code", want: registry.HarnessClaude},
		{name: "claude alias underscore", value: "claude_code", want: registry.HarnessClaude},
		{name: "grok alias hyphen", value: "grok-build", want: registry.HarnessGrok},
		{name: "grok alias underscore", value: "grok_build", want: registry.HarnessGrok},
		{name: "opencode alias hyphen", value: "open-code", want: registry.HarnessOpenCode},
		{name: "opencode alias underscore", value: "open_code", want: registry.HarnessOpenCode},
		{name: "agy alias", value: "antigravity-cli", want: registry.HarnessAgy},
		{name: "agy google alias", value: "google_antigravity", want: registry.HarnessAgy},
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

	want := []string{"claude", codexCommand, "grok", "pi", "opencode", "agy"}
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
			},
		},
		{
			name:  "event",
			field: EnvEvent,
			want: []string{
				"AGENT_SESSIONS_EVENT",
				"AGENT_EVENT",
				"GROK_HOOK_EVENT",
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
		{command: "claude", want: registry.HarnessClaude, wantOK: true},
		{command: "grok", want: registry.HarnessGrok, wantOK: true},
		{command: "grok-build", want: registry.HarnessGrok, wantOK: true},
		{command: "pi", want: registry.HarnessPi, wantOK: true},
		{command: "opencode", want: registry.HarnessOpenCode, wantOK: true},
		{command: "agy", want: registry.HarnessAgy, wantOK: true},
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
