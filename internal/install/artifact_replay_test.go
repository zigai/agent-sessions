package install

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

type replayEnv struct {
	root      string
	home      string
	binary    string
	storePath string
}

type replayResult struct {
	stdout string
	stderr string
}

type jsonReplayCase struct {
	event        string
	payload      string
	wantState    registry.State
	wantID       string
	wantPath     string
	wantCWD      string
	wantRoot     string
	wantEvent    string
	wantAttrs    map[string]string
	wantRaw      bool
	wantExited   bool
	wantCommands int
}

type cursorReplayCase struct {
	event      string
	payload    string
	wantOutput string
	wantState  registry.State
	wantEvent  string
	wantAttrs  map[string]string
}

type tsReplayCase struct {
	name       string
	handler    string
	payload    string
	wantState  registry.State
	wantID     string
	wantPath   string
	wantCWD    string
	wantRoot   string
	wantEvent  string
	wantAttrs  map[string]string
	wantExited bool
}

//nolint:exhaustruct,maintidx // Artifact replay tables intentionally omit zero-value expectations for readability.
func TestArtifactReplayJSONCommandHooks(t *testing.T) {
	env := newReplayEnv(t)

	tests := []struct {
		name        string
		harness     registry.Harness
		setup       func(*testing.T, replayEnv)
		cases       []jsonReplayCase
		selfRefresh []string
	}{
		{
			name:    "codex",
			harness: registry.HarnessCodex,
			setup: func(t *testing.T, env replayEnv) {
				t.Helper()
				t.Setenv("CODEX_HOME", filepath.Join(env.home, ".codex"))
			},
			cases: []jsonReplayCase{
				{
					event:     hookEventSessionStart,
					payload:   `{"session_id":"codex-replay","transcript_path":"/tmp/.codex/sessions/replay.jsonl","cwd":"/repo/codex","hook_event_name":"SessionStart","source":"startup","model":"gpt-5-codex"}`,
					wantState: registry.StateIdle,
					wantID:    "codex-replay",
					wantPath:  "/tmp/.codex/sessions/replay.jsonl",
					wantCWD:   "/repo/codex",
					wantEvent: hookEventSessionStart,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "codex-hook",
						"codex_hook_event":           "SessionStart",
						"codex_start_source":         "startup",
						"codex_model":                "gpt-5-codex",
					},
					wantRaw: true,
				},
				{
					event:     "UserPromptSubmit",
					payload:   `{"session_id":"codex-replay","transcript_path":"/tmp/.codex/sessions/replay.jsonl","cwd":"/repo/codex","hook_event_name":"UserPromptSubmit","model":"gpt-5-codex","turn_id":"turn-1"}`,
					wantState: registry.StateRunning,
					wantID:    "codex-replay",
					wantPath:  "/tmp/.codex/sessions/replay.jsonl",
					wantCWD:   "/repo/codex",
					wantEvent: "UserPromptSubmit",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "codex-hook",
						"codex_hook_event":           "UserPromptSubmit",
						"codex_model":                "gpt-5-codex",
						"codex_turn_id":              "turn-1",
					},
					wantRaw: true,
				},
				{
					event:     "PermissionRequest",
					payload:   `{"session_id":"codex-replay","transcript_path":"/tmp/.codex/sessions/replay.jsonl","cwd":"/repo/codex","hook_event_name":"PermissionRequest","model":"gpt-5-codex","permission_mode":"on-request"}`,
					wantState: registry.StateWaiting,
					wantID:    "codex-replay",
					wantPath:  "/tmp/.codex/sessions/replay.jsonl",
					wantCWD:   "/repo/codex",
					wantEvent: "PermissionRequest",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "codex-hook",
						"codex_hook_event":           "PermissionRequest",
						"codex_permission_mode":      "on-request",
					},
					wantRaw: true,
				},
				{
					event:     hookEventStop,
					payload:   `{"session_id":"codex-replay","transcript_path":"/tmp/.codex/sessions/replay.jsonl","cwd":"/repo/codex","hook_event_name":"Stop","model":"gpt-5-codex"}`,
					wantState: registry.StateIdle,
					wantID:    "codex-replay",
					wantPath:  "/tmp/.codex/sessions/replay.jsonl",
					wantCWD:   "/repo/codex",
					wantEvent: hookEventStop,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "codex-hook",
						"codex_hook_event":           "Stop",
					},
					wantRaw: true,
				},
			},
		},
		{
			name:    "claude",
			harness: registry.HarnessClaude,
			setup: func(t *testing.T, env replayEnv) {
				t.Helper()
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(env.home, ".claude"))
			},
			cases: []jsonReplayCase{
				{
					event:     hookEventSessionStart,
					payload:   `{"session_id":"claude-replay","transcript_path":"/tmp/.claude/projects/repo/claude-replay.jsonl","cwd":"/repo/claude","hook_event_name":"SessionStart","source":"startup","model":"claude-sonnet-4-6"}`,
					wantState: registry.StateIdle,
					wantID:    "claude-replay",
					wantPath:  "/tmp/.claude/projects/repo/claude-replay.jsonl",
					wantCWD:   "/repo/claude",
					wantEvent: hookEventSessionStart,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "claude-hook",
						"claude_hook_event":          "SessionStart",
						"claude_start_source":        "startup",
						"claude_model":               "claude-sonnet-4-6",
					},
					wantRaw: true,
				},
				{
					event:     "UserPromptSubmit",
					payload:   `{"session_id":"claude-replay","transcript_path":"/tmp/.claude/projects/repo/claude-replay.jsonl","cwd":"/repo/claude","hook_event_name":"UserPromptSubmit","model":"claude-sonnet-4-6"}`,
					wantState: registry.StateRunning,
					wantID:    "claude-replay",
					wantPath:  "/tmp/.claude/projects/repo/claude-replay.jsonl",
					wantCWD:   "/repo/claude",
					wantEvent: "UserPromptSubmit",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "claude-hook",
						"claude_hook_event":          "UserPromptSubmit",
					},
					wantRaw: true,
				},
				{
					event:     "Notification",
					payload:   `{"session_id":"claude-replay","transcript_path":"/tmp/.claude/projects/repo/claude-replay.jsonl","cwd":"/repo/claude","hook_event_name":"Notification","notification_type":"permission_prompt"}`,
					wantState: registry.StateWaiting,
					wantID:    "claude-replay",
					wantPath:  "/tmp/.claude/projects/repo/claude-replay.jsonl",
					wantCWD:   "/repo/claude",
					wantEvent: "Notification",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "claude-hook",
						"claude_hook_event":          "Notification",
						"claude_notification_type":   "permission_prompt",
					},
					wantRaw: true,
				},
				{
					event:     hookEventStop,
					payload:   `{"session_id":"claude-replay","transcript_path":"/tmp/.claude/projects/repo/claude-replay.jsonl","cwd":"/repo/claude","hook_event_name":"Stop","reason":"end_turn"}`,
					wantState: registry.StateIdle,
					wantID:    "claude-replay",
					wantPath:  "/tmp/.claude/projects/repo/claude-replay.jsonl",
					wantCWD:   "/repo/claude",
					wantEvent: hookEventStop,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "claude-hook",
						"claude_hook_event":          "Stop",
					},
					wantRaw: true,
				},
				{
					event:      "SessionEnd",
					payload:    `{"session_id":"claude-replay","transcript_path":"/tmp/.claude/projects/repo/claude-replay.jsonl","cwd":"/repo/claude","hook_event_name":"SessionEnd","reason":"exit"}`,
					wantState:  registry.StateExited,
					wantID:     "claude-replay",
					wantPath:   "/tmp/.claude/projects/repo/claude-replay.jsonl",
					wantCWD:    "/repo/claude",
					wantEvent:  "SessionEnd",
					wantAttrs:  map[string]string{"agent_sessions_integration": "claude-hook", "claude_hook_event": "SessionEnd", "claude_session_end_reason": "exit"},
					wantRaw:    true,
					wantExited: true,
				},
			},
		},
		{
			name:    "grok",
			harness: registry.HarnessGrok,
			setup: func(t *testing.T, env replayEnv) {
				t.Helper()
				t.Setenv("GROK_HOME", filepath.Join(env.home, ".grok"))
			},
			selfRefresh: []string{hookEventSessionStart},
			cases: []jsonReplayCase{
				{
					event:        hookEventSessionStart,
					payload:      `{"sessionId":"grok-replay","cwd":"/repo/grok","workspaceRoot":"/repo","hookEventName":"SessionStart"}`,
					wantState:    registry.StateIdle,
					wantID:       "grok-replay",
					wantCWD:      "/repo/grok",
					wantRoot:     "/repo",
					wantEvent:    hookEventSessionStart,
					wantAttrs:    map[string]string{"agent_sessions_integration": "grok-hook", "grok_hook_event": "SessionStart"},
					wantRaw:      true,
					wantCommands: 2,
				},
				{
					event:     "UserPromptSubmit",
					payload:   `{"sessionId":"grok-replay","cwd":"/repo/grok","workspaceRoot":"/repo","hookEventName":"UserPromptSubmit","toolName":"run_terminal_command"}`,
					wantState: registry.StateRunning,
					wantID:    "grok-replay",
					wantCWD:   "/repo/grok",
					wantRoot:  "/repo",
					wantEvent: "UserPromptSubmit",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "grok-hook",
						"grok_hook_event":            "UserPromptSubmit",
						"grok_tool_name":             "run_terminal_command",
					},
					wantRaw: true,
				},
				{
					event:     "Notification",
					payload:   `{"sessionId":"grok-replay","cwd":"/repo/grok","workspaceRoot":"/repo","hookEventName":"Notification","notificationType":"approval_required"}`,
					wantState: registry.StateWaiting,
					wantID:    "grok-replay",
					wantCWD:   "/repo/grok",
					wantRoot:  "/repo",
					wantEvent: "Notification",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "grok-hook",
						"grok_hook_event":            "Notification",
						"grok_notification_type":     "approval_required",
					},
					wantRaw: true,
				},
				{
					event:     hookEventStop,
					payload:   `{"sessionId":"grok-replay","cwd":"/repo/grok","workspaceRoot":"/repo","hookEventName":"Stop"}`,
					wantState: registry.StateIdle,
					wantID:    "grok-replay",
					wantCWD:   "/repo/grok",
					wantRoot:  "/repo",
					wantEvent: hookEventStop,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "grok-hook",
						"grok_hook_event":            "Stop",
					},
					wantRaw: true,
				},
				{
					event:      "SessionEnd",
					payload:    `{"sessionId":"grok-replay","cwd":"/repo/grok","workspaceRoot":"/repo","hookEventName":"SessionEnd"}`,
					wantState:  registry.StateExited,
					wantID:     "grok-replay",
					wantCWD:    "/repo/grok",
					wantRoot:   "/repo",
					wantEvent:  "SessionEnd",
					wantAttrs:  map[string]string{"agent_sessions_integration": "grok-hook", "grok_hook_event": "SessionEnd"},
					wantRaw:    true,
					wantExited: true,
				},
			},
		},
		{
			name:    "droid",
			harness: registry.HarnessDroid,
			setup: func(t *testing.T, env replayEnv) {
				t.Helper()
				t.Setenv("HOME", env.home)
			},
			selfRefresh: []string{hookEventSessionStart},
			cases: []jsonReplayCase{
				{
					event:     hookEventSessionStart,
					payload:   `{"session_id":"droid-replay","transcript_path":"/tmp/droid-replay.jsonl","cwd":"/repo/droid","hook_event_name":"SessionStart","source":"startup"}`,
					wantState: registry.StateIdle,
					wantID:    "droid-replay",
					wantPath:  "/tmp/droid-replay.jsonl",
					wantCWD:   "/repo/droid",
					wantEvent: hookEventSessionStart,
					wantAttrs: map[string]string{
						"agent_sessions_integration": "droid-hook",
						"droid_hook_event":           "SessionStart",
						"droid_source":               "startup",
					},
				},
				{
					event:     "PreToolUse",
					payload:   `{"session_id":"droid-replay","transcript_path":"/tmp/droid-replay.jsonl","cwd":"/repo/droid","hook_event_name":"PreToolUse","tool_name":"Bash"}`,
					wantState: registry.StateRunning,
					wantID:    "droid-replay",
					wantPath:  "/tmp/droid-replay.jsonl",
					wantCWD:   "/repo/droid",
					wantEvent: "PreToolUse",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "droid-hook",
						"droid_hook_event":           "PreToolUse",
						"droid_tool_name":            "Bash",
					},
				},
				{
					event:     "Notification",
					payload:   `{"session_id":"droid-replay","transcript_path":"/tmp/droid-replay.jsonl","cwd":"/repo/droid","hook_event_name":"Notification","permission_mode":"on-request"}`,
					wantState: registry.StateWaiting,
					wantID:    "droid-replay",
					wantPath:  "/tmp/droid-replay.jsonl",
					wantCWD:   "/repo/droid",
					wantEvent: "Notification",
					wantAttrs: map[string]string{
						"agent_sessions_integration": "droid-hook",
						"droid_hook_event":           "Notification",
						"droid_permission_mode":      "on-request",
					},
				},
				{
					event:      "SessionEnd",
					payload:    `{"session_id":"droid-replay","transcript_path":"/tmp/droid-replay.jsonl","cwd":"/repo/droid","hook_event_name":"SessionEnd","reason":"exit"}`,
					wantState:  registry.StateExited,
					wantID:     "droid-replay",
					wantPath:   "/tmp/droid-replay.jsonl",
					wantCWD:    "/repo/droid",
					wantEvent:  "SessionEnd",
					wantAttrs:  map[string]string{"agent_sessions_integration": "droid-hook", "droid_hook_event": "SessionEnd", "droid_reason": "exit"},
					wantExited: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, env)
			resetReplayStore(t, env)

			result := installReplayHarness(t, test.harness, env)
			commands := readJSONCommandHooks(t, result.Path)
			for _, event := range test.selfRefresh {
				selfRefreshCommand := requireCommandContaining(t, commands[event], " install-hooks ")
				got := runReplayCommand(t, env, selfRefreshCommand, "{}")
				requireReplayOutput(t, got, "")
				requireNoReplaySessions(t, env.storePath, test.harness)
			}

			for _, replay := range test.cases {
				eventCommands := commands[replay.event]
				if replay.wantCommands > 0 && len(eventCommands) != replay.wantCommands {
					t.Fatalf("expected %d commands for %s, got %#v", replay.wantCommands, replay.event, eventCommands)
				}
				command := requireCommandContaining(t, eventCommands, " report ")
				got := runReplayCommand(t, env, command, replay.payload)
				requireReplayOutput(t, got, "")
				session := waitForReplaySession(t, env.storePath, test.harness, replay.wantState, replay.wantEvent)
				requireReplaySessionFields(t, session, replay)
			}
		})
	}
}

func TestArtifactReplayCursorHooks(t *testing.T) {
	env := newReplayEnv(t)
	t.Setenv("HOME", env.home)
	resetReplayStore(t, env)

	result := installReplayHarness(t, registry.HarnessCursor, env)
	commands := readCursorCommandHooks(t, result.Path)
	replayPayload := func(event string) string {
		return fmt.Sprintf(
			`{"session_id":"cursor-replay","transcript_path":"/tmp/cursor-replay.jsonl","workspace_roots":["/repo/cursor"],"hook_event_name":%s,"model":"gpt-5.2","cursor_version":"2026.06.15","composer_mode":"agent","prompt":"must not be stored"}`,
			strconv.Quote(event),
		)
	}

	tests := []cursorReplayCase{
		{
			event:      "sessionStart",
			payload:    replayPayload("sessionStart"),
			wantOutput: "{}\n",
			wantState:  registry.StateIdle,
			wantEvent:  "sessionStart",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cursor-hook", "cursor_hook_event": "sessionStart"},
		},
		{
			event:      "beforeSubmitPrompt",
			payload:    replayPayload("beforeSubmitPrompt"),
			wantOutput: "{\"continue\":true}\n",
			wantState:  registry.StateRunning,
			wantEvent:  "beforeSubmitPrompt",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "cursor-hook",
				"cursor_hook_event":          "beforeSubmitPrompt",
				"cursor_model":               "gpt-5.2",
				"cursor_version":             "2026.06.15",
				"cursor_composer_mode":       "agent",
			},
		},
		{
			event:      "beforeShellExecution",
			payload:    replayPayload("beforeShellExecution"),
			wantOutput: "{\"permission\":\"allow\"}\n",
			wantState:  registry.StateWaiting,
			wantEvent:  "beforeShellExecution",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cursor-hook", "cursor_hook_event": "beforeShellExecution"},
		},
		{
			event:      "afterShellExecution",
			payload:    replayPayload("afterShellExecution"),
			wantOutput: "{}\n",
			wantState:  registry.StateRunning,
			wantEvent:  "afterShellExecution",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cursor-hook", "cursor_hook_event": "afterShellExecution"},
		},
		{
			event:      "stop",
			payload:    replayPayload("stop"),
			wantOutput: "{}\n",
			wantState:  registry.StateIdle,
			wantEvent:  "stop",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cursor-hook", "cursor_hook_event": "stop"},
		},
		{
			event:      "sessionEnd",
			payload:    replayPayload("sessionEnd"),
			wantOutput: "{}\n",
			wantState:  registry.StateExited,
			wantEvent:  "sessionEnd",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cursor-hook", "cursor_hook_event": "sessionEnd"},
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			got := runReplayCommand(t, env, commands[test.event], test.payload)
			requireReplayOutput(t, got, test.wantOutput)

			session := waitForReplaySession(t, env.storePath, registry.HarnessCursor, test.wantState, test.wantEvent)
			if session.SessionID != "cursor-replay" {
				t.Fatalf("expected cursor session id, got %q", session.SessionID)
			}
			if session.SessionPath != "/tmp/cursor-replay.jsonl" {
				t.Fatalf("expected cursor transcript path, got %q", session.SessionPath)
			}
			if session.CWD != "/repo/cursor" || session.ProjectRoot != "/repo/cursor" {
				t.Fatalf("expected cursor cwd/root from workspace roots, got cwd=%q root=%q", session.CWD, session.ProjectRoot)
			}
			if session.State != test.wantState || session.LastEvent != test.wantEvent {
				t.Fatalf("expected state=%s event=%s, got state=%s event=%s", test.wantState, test.wantEvent, session.State, session.LastEvent)
			}
			requireReplayAttributes(t, session, test.wantAttrs)
			if len(session.RawPayload) != 0 {
				t.Fatalf("expected cursor defaults-only hook not to store raw payload, got %s", session.RawPayload)
			}
		})
	}

	t.Run("invalid payload still returns protocol response without report", func(t *testing.T) {
		resetReplayStore(t, env)
		got := runReplayCommand(t, env, commands["beforeSubmitPrompt"], `{"session_id":"cursor-replay","hook_event_name":"beforeSubmitPrompt"}`)
		requireReplayOutput(t, got, "{\"continue\":true}\n")
		requireNoReplaySessions(t, env.storePath, registry.HarnessCursor)
	})
}

//nolint:exhaustruct // Replay cases intentionally omit zero-value expectations for readability.
func TestArtifactReplayCopilotHooks(t *testing.T) {
	env := newReplayEnv(t)
	t.Setenv("COPILOT_HOME", filepath.Join(env.home, ".copilot"))
	resetReplayStore(t, env)

	result := installReplayHarness(t, registry.HarnessCopilot, env)
	commands := readCopilotCommandHooks(t, result.Path)
	tests := []jsonReplayCase{
		{
			event:     "sessionStart",
			payload:   `{"sessionId":"copilot-replay","timestamp":"2026-06-29T10:00:00Z","cwd":"/repo/copilot","source":"startup"}`,
			wantState: registry.StateIdle,
			wantID:    "copilot-replay",
			wantCWD:   "/repo/copilot",
			wantEvent: "sessionStart",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "copilot-hook",
				"copilot_hook_event":         "sessionStart",
				"copilot_start_source":       "startup",
			},
		},
		{
			event:     "preToolUse",
			payload:   `{"sessionId":"copilot-replay","timestamp":"2026-06-29T10:00:01Z","cwd":"/repo/copilot","toolName":"Bash","toolArgs":{"command":"echo hi"}}`,
			wantState: registry.StateRunning,
			wantID:    "copilot-replay",
			wantCWD:   "/repo/copilot",
			wantEvent: "preToolUse",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "copilot-hook",
				"copilot_hook_event":         "preToolUse",
				"copilot_tool_name":          "Bash",
			},
		},
		{
			event:     "permissionRequest",
			payload:   `{"sessionId":"copilot-replay","timestamp":"2026-06-29T10:00:02Z","cwd":"/repo/copilot","toolName":"Bash"}`,
			wantState: registry.StateWaiting,
			wantID:    "copilot-replay",
			wantCWD:   "/repo/copilot",
			wantEvent: "permissionRequest",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "copilot-hook",
				"copilot_hook_event":         "permissionRequest",
				"copilot_tool_name":          "Bash",
			},
		},
		{
			event:      "sessionEnd",
			payload:    `{"sessionId":"copilot-replay","timestamp":"2026-06-29T10:00:03Z","cwd":"/repo/copilot","reason":"exit"}`,
			wantState:  registry.StateExited,
			wantID:     "copilot-replay",
			wantCWD:    "/repo/copilot",
			wantEvent:  "sessionEnd",
			wantAttrs:  map[string]string{"agent_sessions_integration": "copilot-hook", "copilot_hook_event": "sessionEnd", "copilot_reason": "exit"},
			wantExited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			got := runReplayCommand(t, env, commands[test.event], test.payload)
			requireReplayOutput(t, got, "")
			session := waitForReplaySession(t, env.storePath, registry.HarnessCopilot, test.wantState, test.wantEvent)
			requireReplaySessionFields(t, session, test)
		})
	}
}

//nolint:exhaustruct // Replay cases intentionally omit zero-value expectations for readability.
func TestArtifactReplayClineHookFiles(t *testing.T) {
	env := newReplayEnv(t)
	hooksDir := filepath.Join(env.home, ".cline", "hooks")
	dataDir := filepath.Join(env.home, ".cline", "data")
	t.Setenv("CLINE_HOOKS_DIR", hooksDir)
	t.Setenv("CLINE_DATA_DIR", dataDir)
	resetReplayStore(t, env)

	result := installReplayHarness(t, registry.HarnessCline, env)
	script := func(name string) string {
		return filepath.Join(result.Path, name+".sh")
	}
	payload := func(hookName string) string {
		return fmt.Sprintf(
			`{"clineVersion":"3.2.1","hookName":%s,"taskId":"cline-task","sessionContext":{"rootSessionId":"cline-replay"},"workspaceRoots":["/repo/cline"],"tool_call":{"name":"execute_command"}}`,
			strconv.Quote(hookName),
		)
	}
	tests := []jsonReplayCase{
		{
			event:     "TaskStart",
			payload:   payload("TaskStart"),
			wantState: registry.StateIdle,
			wantID:    "cline-replay",
			wantPath:  filepath.Join(dataDir, "sessions", "cline-replay", "cline-replay.messages.json"),
			wantCWD:   "/repo/cline",
			wantRoot:  "/repo/cline",
			wantEvent: "TaskStart",
			wantAttrs: map[string]string{"agent_sessions_integration": "cline-hook", "cline_hook_event": "TaskStart"},
		},
		{
			event:     "UserPromptSubmit",
			payload:   payload("UserPromptSubmit"),
			wantState: registry.StateRunning,
			wantID:    "cline-replay",
			wantPath:  filepath.Join(dataDir, "sessions", "cline-replay", "cline-replay.messages.json"),
			wantCWD:   "/repo/cline",
			wantRoot:  "/repo/cline",
			wantEvent: "UserPromptSubmit",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "cline-hook",
				"cline_hook_event":           "UserPromptSubmit",
				"cline_tool_name":            "execute_command",
			},
		},
		{
			event:     "PreToolUse",
			payload:   payload("PreToolUse"),
			wantState: registry.StateRunning,
			wantID:    "cline-replay",
			wantPath:  filepath.Join(dataDir, "sessions", "cline-replay", "cline-replay.messages.json"),
			wantCWD:   "/repo/cline",
			wantRoot:  "/repo/cline",
			wantEvent: "PreToolUse",
			wantAttrs: map[string]string{"agent_sessions_integration": "cline-hook", "cline_hook_event": "PreToolUse"},
		},
		{
			event:      "SessionShutdown",
			payload:    payload("SessionShutdown"),
			wantState:  registry.StateExited,
			wantID:     "cline-replay",
			wantPath:   filepath.Join(dataDir, "sessions", "cline-replay", "cline-replay.messages.json"),
			wantCWD:    "/repo/cline",
			wantRoot:   "/repo/cline",
			wantEvent:  "SessionShutdown",
			wantAttrs:  map[string]string{"agent_sessions_integration": "cline-hook", "cline_hook_event": "SessionShutdown"},
			wantExited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			got := runReplayCommand(t, env, "sh "+strconv.Quote(script(test.event)), test.payload)
			requireReplayOutput(t, got, "{}\n")
			session := waitForReplaySession(t, env.storePath, registry.HarnessCline, test.wantState, test.wantEvent)
			requireReplaySessionFields(t, session, test)
		})
	}

	t.Run("invalid payload still returns cline protocol response without report", func(t *testing.T) {
		resetReplayStore(t, env)
		got := runReplayCommand(t, env, "sh "+strconv.Quote(script("UserPromptSubmit")), `{"hookName":"UserPromptSubmit"}`)
		requireReplayOutput(t, got, "{}\n")
		requireNoReplaySessions(t, env.storePath, registry.HarnessCline)
	})
}

//nolint:exhaustruct // Replay cases intentionally omit zero-value expectations for readability.
func TestArtifactReplayGoosePluginHooks(t *testing.T) {
	env := newReplayEnv(t)
	t.Setenv("HOME", env.home)
	resetReplayStore(t, env)

	result := installReplayHarness(t, registry.HarnessGoose, env)
	commands := readGoosePluginCommands(t, result.Path)

	selfRefreshCommand := requireCommandContaining(t, commands[hookEventSessionStart], " install-hooks ")
	selfRefreshGot := runGooseReplayCommand(t, env, result.Path, selfRefreshCommand, "{}")
	requireReplayOutput(t, selfRefreshGot, "")
	requireNoReplaySessions(t, env.storePath, registry.HarnessGoose)

	tests := []jsonReplayCase{
		{
			event:     hookEventSessionStart,
			payload:   `{"event":"SessionStart","session_id":"goose-replay","working_dir":"/repo/goose"}`,
			wantState: registry.StateIdle,
			wantID:    "goose-replay",
			wantCWD:   "/repo/goose",
			wantRoot:  "/repo/goose",
			wantEvent: hookEventSessionStart,
			wantAttrs: map[string]string{"agent_sessions_integration": "goose-hook", "goose_event": "SessionStart"},
		},
		{
			event:     "UserPromptSubmit",
			payload:   `{"event":"UserPromptSubmit","session_id":"goose-replay","working_dir":"/repo/goose","message":"must not be stored"}`,
			wantState: registry.StateRunning,
			wantID:    "goose-replay",
			wantCWD:   "/repo/goose",
			wantRoot:  "/repo/goose",
			wantEvent: "UserPromptSubmit",
			wantAttrs: map[string]string{"agent_sessions_integration": "goose-hook", "goose_event": "UserPromptSubmit"},
		},
		{
			event:     "PreToolUse",
			payload:   `{"event":"PreToolUse","session_id":"goose-replay","working_dir":"/repo/goose","tool_name":"shell","matcher_context":"*","tool_input":{"command":"echo hi"}}`,
			wantState: registry.StateRunning,
			wantID:    "goose-replay",
			wantCWD:   "/repo/goose",
			wantRoot:  "/repo/goose",
			wantEvent: "PreToolUse",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "goose-hook",
				"goose_event":                "PreToolUse",
				"goose_tool_name":            "shell",
				"goose_matcher_context":      "*",
			},
		},
		{
			event:      "SessionEnd",
			payload:    `{"event":"SessionEnd","session_id":"goose-replay","working_dir":"/repo/goose"}`,
			wantState:  registry.StateExited,
			wantID:     "goose-replay",
			wantCWD:    "/repo/goose",
			wantRoot:   "/repo/goose",
			wantEvent:  "SessionEnd",
			wantAttrs:  map[string]string{"agent_sessions_integration": "goose-hook", "goose_event": "SessionEnd"},
			wantExited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			command := requireCommandContaining(t, commands[test.event], "report.sh")
			got := runGooseReplayCommand(t, env, result.Path, command, test.payload)
			requireReplayOutput(t, got, "")
			session := waitForReplaySession(t, env.storePath, registry.HarnessGoose, test.wantState, test.wantEvent)
			requireReplaySessionFields(t, session, test)
		})
	}
}

//nolint:exhaustruct // Kimi replay cases intentionally omit zero-value expectations.
func TestArtifactReplayKimiManagedTextHooks(t *testing.T) {
	env := newReplayEnv(t)
	kimiHome := filepath.Join(env.home, ".kimi-code")
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	resetReplayStore(t, env)

	sessionDir := filepath.Join(kimiHome, "sessions", "wd_repo", "kimi-replay")
	index := `{"sessionId":"kimi-replay","sessionDir":` + strconv.Quote(sessionDir) + `,"workDir":"/repo/kimi"}` + "\n"
	if err := os.MkdirAll(kimiHome, 0o700); err != nil {
		t.Fatalf("creating kimi home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kimiHome, "session_index.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatalf("writing kimi session index: %v", err)
	}

	result := installReplayHarness(t, registry.HarnessKimiCode, env)
	commands := readKimiManagedTextCommands(t, result.Path)

	selfRefreshCommand := requireCommandContaining(t, commands[hookEventSessionStart], " install-hooks ")
	selfRefreshGot := runReplayCommand(t, env, selfRefreshCommand, "{}")
	requireReplayOutput(t, selfRefreshGot, "")
	requireNoReplaySessions(t, env.storePath, registry.HarnessKimiCode)

	tests := []jsonReplayCase{
		{
			event:     hookEventSessionStart,
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"SessionStart","source":"startup"}`,
			wantState: registry.StateIdle,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: hookEventSessionStart,
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "SessionStart",
				"kimi_code_start_source":     "startup",
			},
			wantRaw: true,
		},
		{
			event:     "UserPromptSubmit",
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"UserPromptSubmit","turn_id":7}`,
			wantState: registry.StateRunning,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: "UserPromptSubmit",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "UserPromptSubmit",
				"kimi_code_turn_id":          "7",
			},
			wantRaw: true,
		},
		{
			event:     "PermissionRequest",
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"PermissionRequest","tool_name":"Bash","decision":"ask"}`,
			wantState: registry.StateWaiting,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: "PermissionRequest",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "PermissionRequest",
				"kimi_code_tool_name":        "Bash",
				"kimi_code_decision":         "ask",
			},
			wantRaw: true,
		},
		{
			event:     "PermissionResult",
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"PermissionResult","tool_name":"Bash","decision":"allow","reason":"approved"}`,
			wantState: registry.StateRunning,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: "PermissionResult",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "PermissionResult",
				"kimi_code_decision":         "allow",
				"kimi_code_reason":           "approved",
			},
			wantRaw: true,
		},
		{
			event:     hookEventStop,
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"Stop"}`,
			wantState: registry.StateIdle,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: hookEventStop,
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "Stop",
			},
			wantRaw: true,
		},
		{
			event:     "StopFailure",
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"StopFailure","reason":"blocked"}`,
			wantState: registry.StateIdle,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: "StopFailure",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "kimi-code-hook",
				"kimi_code_hook_event":       "StopFailure",
				"kimi_code_reason":           "blocked",
			},
			wantRaw: true,
		},
		{
			event:     "Interrupt",
			payload:   `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"Interrupt","notification_type":"user_interrupt"}`,
			wantState: registry.StateIdle,
			wantID:    "kimi-replay",
			wantPath:  sessionDir,
			wantCWD:   "/repo/kimi",
			wantEvent: "Interrupt",
			wantAttrs: map[string]string{
				"agent_sessions_integration":  "kimi-code-hook",
				"kimi_code_hook_event":        "Interrupt",
				"kimi_code_notification_type": "user_interrupt",
			},
			wantRaw: true,
		},
		{
			event:      "SessionEnd",
			payload:    `{"session_id":"kimi-replay","cwd":"/repo/kimi","hook_event_name":"SessionEnd"}`,
			wantState:  registry.StateExited,
			wantID:     "kimi-replay",
			wantPath:   sessionDir,
			wantCWD:    "/repo/kimi",
			wantEvent:  "SessionEnd",
			wantAttrs:  map[string]string{"agent_sessions_integration": "kimi-code-hook", "kimi_code_hook_event": "SessionEnd"},
			wantRaw:    true,
			wantExited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			command := requireCommandContaining(t, commands[test.event], " report ")
			replayGot := runReplayCommand(t, env, command, test.payload)
			requireReplayOutput(t, replayGot, "")
			session := waitForReplaySession(t, env.storePath, registry.HarnessKimiCode, test.wantState, test.wantEvent)
			requireReplaySessionFields(t, session, test)
		})
	}
}

//nolint:gocognit // Agy replay test intentionally covers all plugin hook state transitions in one flow.
func TestArtifactReplayAgyPluginHooks(t *testing.T) {
	env := newReplayEnv(t)
	t.Setenv("AGY_CONFIG_HOME", filepath.Join(env.home, ".gemini", "config"))
	resetReplayStore(t, env)

	result := installReplayHarness(t, registry.HarnessAgy, env)
	commands := readAgyPluginCommands(t, result.Path)

	tests := []struct {
		event      string
		payload    string
		wantOutput string
		wantState  registry.State
		wantCWD    string
		wantEvent  string
		wantAttrs  map[string]string
	}{
		{
			event:      "PreInvocation",
			payload:    `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"invocationNum":1}`,
			wantOutput: "{}\n",
			wantState:  registry.StateRunning,
			wantCWD:    "/repo",
			wantEvent:  "PreInvocation",
			wantAttrs:  map[string]string{"agent_sessions_integration": "agy-hook", "agy_hook_event": "PreInvocation"},
		},
		{
			event:      "PostInvocation",
			payload:    `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"invocationNum":1}`,
			wantOutput: "{}\n",
			wantState:  registry.StateRunning,
			wantCWD:    "/repo",
			wantEvent:  "PostInvocation",
			wantAttrs:  map[string]string{"agent_sessions_integration": "agy-hook", "agy_hook_event": "PostInvocation"},
		},
		{
			event:      "PreToolUse",
			payload:    `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"toolCall":{"name":"ask_permission","args":{"Cwd":"/repo/pkg"}}}`,
			wantOutput: `{"decision":"allow"}`,
			wantState:  registry.StateWaiting,
			wantCWD:    "/repo/pkg",
			wantEvent:  "PreToolUse",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "agy-hook",
				"agy_hook_event":             "PreToolUse",
				"agy_tool_name":              "ask_permission",
			},
		},
		{
			event:      "PostToolUse",
			payload:    `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"toolCall":{"name":"run_command","args":{"Cwd":"/repo/cmd"}}}`,
			wantOutput: "{}\n",
			wantState:  registry.StateRunning,
			wantCWD:    "/repo/cmd",
			wantEvent:  "PostToolUse",
			wantAttrs: map[string]string{
				"agent_sessions_integration": "agy-hook",
				"agy_hook_event":             "PostToolUse",
				"agy_tool_name":              "run_command",
			},
		},
		{
			event:      hookEventStop,
			payload:    `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"terminationReason":"model_stop","fullyIdle":true}`,
			wantOutput: `{"decision":""}`,
			wantState:  registry.StateIdle,
			wantCWD:    "/repo",
			wantEvent:  hookEventStop,
			wantAttrs: map[string]string{
				"agent_sessions_integration": "agy-hook",
				"agy_hook_event":             "Stop",
				"agy_termination_reason":     "model_stop",
				"agy_fully_idle":             "true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			got := runReplayCommand(t, env, commands[test.event], test.payload)
			requireReplayJSONOutput(t, got, test.wantOutput)
			session := waitForReplaySession(t, env.storePath, registry.HarnessAgy, test.wantState, test.wantEvent)
			if session.SessionID != "agy-replay" {
				t.Fatalf("expected agy session id, got %q", session.SessionID)
			}
			if session.SessionPath != "/repo/.gemini/antigravity/transcript.jsonl" {
				t.Fatalf("expected agy transcript path, got %q", session.SessionPath)
			}
			if session.CWD != test.wantCWD || session.ProjectRoot != "/repo" {
				t.Fatalf("expected agy cwd/root %q /repo, got cwd=%q root=%q", test.wantCWD, session.CWD, session.ProjectRoot)
			}
			if session.State != test.wantState || session.LastEvent != test.wantEvent {
				t.Fatalf("expected state=%s event=%s, got state=%s event=%s", test.wantState, test.wantEvent, session.State, session.LastEvent)
			}
			requireReplayAttributes(t, session, test.wantAttrs)
			if len(session.RawPayload) == 0 {
				t.Fatal("expected agy hook to store raw payload")
			}
		})
	}

	t.Run("empty post tool use preserves idle stop", func(t *testing.T) {
		got := runReplayCommand(t, env, commands["PostToolUse"], `{"conversationId":"agy-replay","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"toolCall":null}`)
		requireReplayJSONOutput(t, got, `{}`)
		session := waitForReplaySession(t, env.storePath, registry.HarnessAgy, registry.StateIdle, hookEventStop)
		if session.State != registry.StateIdle || session.LastEvent != hookEventStop {
			t.Fatalf("expected empty PostToolUse not to overwrite idle Stop, got state=%s event=%s", session.State, session.LastEvent)
		}
	})

	t.Run("malformed pre tool use still returns allow without report", func(t *testing.T) {
		resetReplayStore(t, env)
		got := runReplayCommand(t, env, commands["PreToolUse"], "not json")
		requireReplayJSONOutput(t, got, `{"decision":"allow"}`)
		requireNoReplaySessions(t, env.storePath, registry.HarnessAgy)
	})
}

//nolint:exhaustruct,maintidx // TypeScript plugin replay tables intentionally omit zero-value expectations for readability.
func TestArtifactReplayTypeScriptPluginsWithBun(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is required to execute generated TypeScript plugin artifacts")
	}

	env := newReplayEnv(t)

	t.Run("opencode", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(env.home, ".config"))
		resetReplayStore(t, env)

		result := installReplayHarness(t, registry.HarnessOpenCode, env)
		cases := []tsReplayCase{
			{
				name:      "session created",
				handler:   "event",
				payload:   `{"type":"session.created","sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateIdle,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "session.created",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "session.created",
				},
			},
			{
				name:      "permission asked",
				handler:   "permission.asked",
				payload:   `{"sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateWaiting,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "permission.asked",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "permission.asked",
				},
			},
			{
				name:      "permission replied",
				handler:   "permission.replied",
				payload:   `{"sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "permission.replied",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "permission.replied",
				},
			},
			{
				name:      "tool execute before",
				handler:   "tool.execute.before",
				payload:   `{"sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "tool.execute.before",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "tool.execute.before",
				},
			},
			{
				name:      "tool execute after",
				handler:   "tool.execute.after",
				payload:   `{"sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "tool.execute.after",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "tool.execute.after",
				},
			},
			{
				name:      "status blocked",
				handler:   "event",
				payload:   `{"type":"session.status","sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl","status":"blocked"}`,
				wantState: registry.StateWaiting,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "session.status",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "session.status",
					"opencode_status":            "blocked",
				},
			},
			{
				name:      "status completed",
				handler:   "event",
				payload:   `{"type":"session.status","sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl","status":"completed"}`,
				wantState: registry.StateIdle,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "session.status",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "session.status",
					"opencode_status":            "completed",
				},
			},
			{
				name:      "session error",
				handler:   "event",
				payload:   `{"type":"session.error","sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState: registry.StateIdle,
				wantID:    "opencode-replay",
				wantPath:  "/tmp/opencode-replay.jsonl",
				wantCWD:   "/repo/opencode",
				wantRoot:  "/repo/opencode",
				wantEvent: "session.error",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "opencode-plugin",
					"opencode_event":             "session.error",
				},
			},
			{
				name:       "session deleted",
				handler:    "event",
				payload:    `{"type":"session.deleted","sessionID":"opencode-replay","sessionPath":"/tmp/opencode-replay.jsonl"}`,
				wantState:  registry.StateExited,
				wantID:     "opencode-replay",
				wantPath:   "/tmp/opencode-replay.jsonl",
				wantCWD:    "/repo/opencode",
				wantRoot:   "/repo/opencode",
				wantEvent:  "session.deleted",
				wantAttrs:  map[string]string{"agent_sessions_integration": "opencode-plugin", "opencode_event": "session.deleted"},
				wantExited: true,
			},
		}

		t.Run("unknown event ignored", func(t *testing.T) {
			runOpenCodePluginEvent(t, bun, env, result.Path, "event", `{"type":"unknown.event","sessionID":"opencode-ignored","sessionPath":"/tmp/opencode-ignored.jsonl"}`)
			time.Sleep(100 * time.Millisecond)
			requireNoReplaySessions(t, env.storePath, registry.HarnessOpenCode)
		})

		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				runOpenCodePluginEvent(t, bun, env, result.Path, test.handler, test.payload)
				session := waitForReplaySession(t, env.storePath, registry.HarnessOpenCode, test.wantState, test.wantEvent)
				requireTypeScriptReplaySessionFields(t, session, test)
			})
		}
	})

	t.Run("kilo", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(env.home, ".config"))
		resetReplayStore(t, env)

		result := installReplayHarness(t, registry.HarnessKilo, env)
		cases := []tsReplayCase{
			{
				name:      "session created",
				handler:   "event",
				payload:   `{"type":"session.created","sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateIdle,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "session.created",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "session.created",
				},
			},
			{
				name:      "permission asked",
				handler:   "permission.asked",
				payload:   `{"sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateWaiting,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "permission.asked",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "permission.asked",
				},
			},
			{
				name:      "permission replied",
				handler:   "permission.replied",
				payload:   `{"sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "permission.replied",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "permission.replied",
				},
			},
			{
				name:      "tool execute before",
				handler:   "tool.execute.before",
				payload:   `{"sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "tool.execute.before",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "tool.execute.before",
				},
			},
			{
				name:      "tool execute after",
				handler:   "tool.execute.after",
				payload:   `{"sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateRunning,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "tool.execute.after",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "tool.execute.after",
				},
			},
			{
				name:      "status active",
				handler:   "event",
				payload:   `{"type":"session.status","sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl","status":"active"}`,
				wantState: registry.StateRunning,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "session.status",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "session.status",
					"kilo_status":                "active",
				},
			},
			{
				name:      "status blocked",
				handler:   "event",
				payload:   `{"type":"session.status","sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl","status":"blocked"}`,
				wantState: registry.StateWaiting,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "session.status",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "session.status",
					"kilo_status":                "blocked",
				},
			},
			{
				name:      "session idle",
				handler:   "event",
				payload:   `{"type":"session.idle","sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState: registry.StateIdle,
				wantID:    "kilo-replay",
				wantPath:  "/tmp/kilo-replay.jsonl",
				wantCWD:   "/repo/kilo",
				wantRoot:  "/repo/kilo",
				wantEvent: "session.idle",
				wantAttrs: map[string]string{
					"agent_sessions_integration": "kilo-plugin",
					"kilo_event":                 "session.idle",
				},
			},
			{
				name:       "session deleted",
				handler:    "event",
				payload:    `{"type":"session.deleted","sessionID":"kilo-replay","sessionPath":"/tmp/kilo-replay.jsonl"}`,
				wantState:  registry.StateExited,
				wantID:     "kilo-replay",
				wantPath:   "/tmp/kilo-replay.jsonl",
				wantCWD:    "/repo/kilo",
				wantRoot:   "/repo/kilo",
				wantEvent:  "session.deleted",
				wantAttrs:  map[string]string{"agent_sessions_integration": "kilo-plugin", "kilo_event": "session.deleted"},
				wantExited: true,
			},
		}

		t.Run("unknown event ignored", func(t *testing.T) {
			runKiloPluginEvent(t, bun, env, result.Path, "event", `{"type":"unknown.event","sessionID":"kilo-ignored","sessionPath":"/tmp/kilo-ignored.jsonl"}`)
			time.Sleep(100 * time.Millisecond)
			requireNoReplaySessions(t, env.storePath, registry.HarnessKilo)
		})

		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				runKiloPluginEvent(t, bun, env, result.Path, test.handler, test.payload)
				session := waitForReplaySession(t, env.storePath, registry.HarnessKilo, test.wantState, test.wantEvent)
				requireTypeScriptReplaySessionFields(t, session, test)
			})
		}
	})

	t.Run("pi", func(t *testing.T) {
		t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(env.home, ".pi", "agent"))
		resetReplayStore(t, env)

		result := installReplayHarness(t, registry.HarnessPi, env)
		cases := []tsReplayCase{
			{
				name:      "session start",
				handler:   "session_start",
				payload:   `{"type":"session_start"}`,
				wantState: registry.StateIdle,
				wantPath:  "/tmp/pi-replay.jsonl",
				wantCWD:   "/repo/pi",
				wantEvent: "session_start",
				wantAttrs: map[string]string{"pi_event": "session_start"},
			},
			{
				name:      "before agent start",
				handler:   "before_agent_start",
				payload:   `{"type":"before_agent_start"}`,
				wantState: registry.StateRunning,
				wantPath:  "/tmp/pi-replay.jsonl",
				wantCWD:   "/repo/pi",
				wantEvent: "before_agent_start",
				wantAttrs: map[string]string{"pi_event": "before_agent_start"},
			},
			{
				name:      "agent start",
				handler:   "agent_start",
				payload:   `{"type":"agent_start"}`,
				wantState: registry.StateRunning,
				wantPath:  "/tmp/pi-replay.jsonl",
				wantCWD:   "/repo/pi",
				wantEvent: "agent_start",
				wantAttrs: map[string]string{"pi_event": "agent_start"},
			},
			{
				name:      "agent end",
				handler:   "agent_end",
				payload:   `{"type":"agent_end","reason":"model_stop"}`,
				wantState: registry.StateIdle,
				wantPath:  "/tmp/pi-replay.jsonl",
				wantCWD:   "/repo/pi",
				wantEvent: "agent_end",
				wantAttrs: map[string]string{
					"pi_event":  "agent_end",
					"pi_reason": "model_stop",
				},
			},
			{
				name:       "session shutdown",
				handler:    "session_shutdown",
				payload:    `{"type":"session_shutdown","reason":"exit"}`,
				wantState:  registry.StateExited,
				wantPath:   "/tmp/pi-replay.jsonl",
				wantCWD:    "/repo/pi",
				wantEvent:  "session_shutdown",
				wantAttrs:  map[string]string{"pi_event": "session_shutdown", "pi_reason": "exit"},
				wantExited: true,
			},
		}

		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				runPiExtensionEvent(t, bun, env, result.Path, test.handler, test.payload)
				session := waitForReplaySession(t, env.storePath, registry.HarnessPi, test.wantState, test.wantEvent)
				requireTypeScriptReplaySessionFields(t, session, test)
				if strings.Join(session.ResumeCommand, " ") != "pi --session /tmp/pi-replay.jsonl" {
					t.Fatalf("expected Pi path resume command, got %#v", session.ResumeCommand)
				}
			})
		}
	})
}

func newReplayEnv(t *testing.T) replayEnv {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin with spaces")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("creating binary directory: %v", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("creating replay home: %v", err)
	}

	binary := filepath.Join(binDir, "agent sessions")
	buildReplayBinary(t, binary)

	return replayEnv{
		root:      root,
		home:      home,
		binary:    binary,
		storePath: filepath.Join(root, "state", "state.json"),
	}
}

func buildReplayBinary(t *testing.T, binary string) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating replay test file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building replay binary: %v\n%s", err, output)
	}
}

func resetReplayStore(t *testing.T, env replayEnv) {
	t.Helper()

	if err := os.Remove(env.storePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing replay store: %v", err)
	}
	if err := os.Remove(env.storePath + ".lock"); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing replay store lock: %v", err)
	}
}

func installReplayHarness(t *testing.T, harness registry.Harness, env replayEnv) Result {
	t.Helper()

	result, err := Run(Options{
		Harness:      harness,
		Binary:       env.binary,
		TargetBinary: "",
		DryRun:       false,
		Force:        false,
		UseShim:      false,
	})
	if err != nil {
		t.Fatalf("installing %s replay hooks: %v", harness, err)
	}
	if result.Path == "" {
		t.Fatalf("expected %s install path", harness)
	}

	return result
}

func runReplayCommand(t *testing.T, env replayEnv, command string, stdin string) replayResult {
	t.Helper()
	if strings.TrimSpace(command) == "" {
		t.Fatal("empty replay command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = env.root
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = replayEnvVars(env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("replay command timed out: %s\nstdout=%s\nstderr=%s", command, stdout.String(), stderr.String())
	}
	if err != nil {
		t.Fatalf("replay command failed: %v\ncommand=%s\nstdout=%s\nstderr=%s", err, command, stdout.String(), stderr.String())
	}

	return replayResult{stdout: stdout.String(), stderr: stderr.String()}
}

func runGooseReplayCommand(t *testing.T, env replayEnv, pluginRoot string, command string, stdin string) replayResult {
	t.Helper()

	prefixedCommand := "PLUGIN_ROOT=" + strconv.Quote(pluginRoot) + "; export PLUGIN_ROOT; " + command

	return runReplayCommand(t, env, prefixedCommand, stdin)
}

func replayEnvVars(env replayEnv) []string {
	values := append([]string(nil), os.Environ()...)
	values = append(values,
		"AGENT_SESSIONS_STORE="+env.storePath,
		"HOME="+env.home,
		"AGENT_SESSIONS_DISABLE_OPENCODE_SELF_REFRESH=1",
		"AGENT_SESSIONS_DISABLE_KILO_SELF_REFRESH=1",
		"AGENT_SESSIONS_DISABLE_PI_SELF_REFRESH=1",
		"TMUX=",
		"TMUX_PANE=",
	)

	return values
}

func requireReplayOutput(t *testing.T, got replayResult, stdout string) {
	t.Helper()

	if got.stdout != stdout || got.stderr != "" {
		t.Fatalf("expected stdout=%q stderr=%q, got stdout=%q stderr=%q", stdout, "", got.stdout, got.stderr)
	}
}

func requireReplayJSONOutput(t *testing.T, got replayResult, want string) {
	t.Helper()

	if got.stderr != "" {
		t.Fatalf("expected empty stderr, got %q", got.stderr)
	}

	var gotValue any
	if err := json.Unmarshal([]byte(got.stdout), &gotValue); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("test expected JSON is invalid: %v\n%s", err, want)
	}
	if !jsonValuesEqual(gotValue, wantValue) {
		t.Fatalf("expected JSON stdout %s, got %s", want, got.stdout)
	}
}

func jsonValuesEqual(left any, right any) bool {
	leftData, leftErr := json.Marshal(left)
	rightData, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftData, rightData)
}

func waitForReplaySession(
	t *testing.T,
	storePath string,
	harness registry.Harness,
	state registry.State,
	event string,
) registry.Session {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var sessions []registry.Session
	var err error
	for time.Now().Before(deadline) {
		sessions, err = registry.NewFileStore(storePath).List(context.Background(), replayHarnessFilter(harness))
		if err == nil && len(sessions) == 1 && sessions[0].State == state && sessions[0].LastEvent == event {
			return sessions[0]
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listing replay sessions: %v", err)
	}
	t.Fatalf("timed out waiting for %s state=%s event=%s, got %#v", harness, state, event, sessions)

	panic("unreachable")
}

func requireNoReplaySessions(t *testing.T, storePath string, harness registry.Harness) {
	t.Helper()

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), replayHarnessFilter(harness))
	if err != nil {
		t.Fatalf("listing replay sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no %s replay sessions, got %#v", harness, sessions)
	}
}

func replayHarnessFilter(harness registry.Harness) registry.Filter {
	return registry.Filter{
		Harness:     harness,
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
		LiveOnly:    false,
	}
}

func requireReplaySessionFields(t *testing.T, session registry.Session, want jsonReplayCase) {
	t.Helper()
	requireJSONReplayIdentity(t, session, want)
	requireJSONReplayLifecycle(t, session, want)
	requireJSONReplayRawPayload(t, session, want.wantRaw)
	requireReplayAttributes(t, session, want.wantAttrs)
	requireReplayTimestamps(t, session)
}

func requireJSONReplayIdentity(t *testing.T, session registry.Session, want jsonReplayCase) {
	t.Helper()
	if session.SessionID != want.wantID {
		t.Fatalf("expected session id %q, got %q", want.wantID, session.SessionID)
	}
	if session.SessionPath != want.wantPath {
		t.Fatalf("expected session path %q, got %q", want.wantPath, session.SessionPath)
	}
	if session.CWD != want.wantCWD {
		t.Fatalf("expected cwd %q, got %q", want.wantCWD, session.CWD)
	}
	if session.ProjectRoot != want.wantRoot {
		t.Fatalf("expected project root %q, got %q", want.wantRoot, session.ProjectRoot)
	}
}

func requireJSONReplayLifecycle(t *testing.T, session registry.Session, want jsonReplayCase) {
	t.Helper()

	if session.State != want.wantState || session.LastEvent != want.wantEvent {
		t.Fatalf("expected state=%s event=%s, got state=%s event=%s", want.wantState, want.wantEvent, session.State, session.LastEvent)
	}
	if want.wantExited && session.EndedAt.IsZero() {
		t.Fatal("expected exited session to have ended timestamp")
	}
}

func requireJSONReplayRawPayload(t *testing.T, session registry.Session, wantRaw bool) {
	t.Helper()

	if wantRaw && len(session.RawPayload) == 0 {
		t.Fatal("expected raw payload to be stored")
	}
	if !wantRaw && len(session.RawPayload) != 0 {
		t.Fatalf("expected raw payload to be empty, got %s", session.RawPayload)
	}
}

func requireReplayTimestamps(t *testing.T, session registry.Session) {
	t.Helper()

	if session.LastEventAt.IsZero() || session.StateChangedAt.IsZero() {
		t.Fatalf("expected event and state timestamps, got event_at=%s state_changed_at=%s", session.LastEventAt, session.StateChangedAt)
	}
}

func requireTypeScriptReplaySessionFields(t *testing.T, session registry.Session, want tsReplayCase) {
	t.Helper()

	if session.SessionID != want.wantID {
		t.Fatalf("expected session id %q, got %q", want.wantID, session.SessionID)
	}
	if session.SessionPath != want.wantPath {
		t.Fatalf("expected session path %q, got %q", want.wantPath, session.SessionPath)
	}
	if session.CWD != want.wantCWD {
		t.Fatalf("expected cwd %q, got %q", want.wantCWD, session.CWD)
	}
	if session.ProjectRoot != want.wantRoot {
		t.Fatalf("expected project root %q, got %q", want.wantRoot, session.ProjectRoot)
	}
	if session.State != want.wantState || session.LastEvent != want.wantEvent {
		t.Fatalf("expected state=%s event=%s, got state=%s event=%s", want.wantState, want.wantEvent, session.State, session.LastEvent)
	}
	if want.wantExited && session.EndedAt.IsZero() {
		t.Fatal("expected exited session to have ended timestamp")
	}
	if len(session.RawPayload) != 0 {
		t.Fatalf("expected TypeScript plugin report not to store raw payload, got %s", session.RawPayload)
	}
	requireReplayAttributes(t, session, want.wantAttrs)
	if session.LastEventAt.IsZero() || session.StateChangedAt.IsZero() {
		t.Fatalf("expected event and state timestamps, got event_at=%s state_changed_at=%s", session.LastEventAt, session.StateChangedAt)
	}
}

func requireReplayAttributes(t *testing.T, session registry.Session, want map[string]string) {
	t.Helper()

	for key, value := range want {
		if session.Attributes[key] != value {
			t.Fatalf("expected attribute %s=%q, got %#v", key, value, session.Attributes)
		}
	}
}

func readJSONCommandHooks(t *testing.T, path string) map[string][]string {
	t.Helper()

	config := decodeTestJSONObject(t, readTestFile(t, path, "reading JSON replay hooks"), "JSON replay hooks")
	hooks := requireTestHooks(t, config)
	commands := make(map[string][]string, len(hooks))
	for event, value := range hooks {
		commands[event] = append(commands[event], jsonCommandsFromHookGroups(t, event, value)...)
	}

	return commands
}

func jsonCommandsFromHookGroups(t *testing.T, event string, value any) []string {
	t.Helper()

	groups, groupsOK := value.([]any)
	if !groupsOK {
		t.Fatalf("expected hook groups for %s, got %#v", event, value)
	}

	commands := make([]string, 0, len(groups))
	for _, groupValue := range groups {
		commands = append(commands, jsonCommandsFromHookGroup(t, event, groupValue)...)
	}

	return commands
}

func jsonCommandsFromHookGroup(t *testing.T, event string, value any) []string {
	t.Helper()

	group, groupOK := value.(map[string]any)
	if !groupOK {
		t.Fatalf("expected hook group object for %s, got %#v", event, value)
	}
	hookValues, hooksOK := group["hooks"].([]any)
	if !hooksOK {
		t.Fatalf("expected nested hooks for %s, got %#v", event, group)
	}

	commands := make([]string, 0, len(hookValues))
	for _, hookValue := range hookValues {
		commands = append(commands, jsonCommandFromHookValue(t, event, hookValue))
	}

	return commands
}

func jsonCommandFromHookValue(t *testing.T, event string, value any) string {
	t.Helper()

	hook, hookOK := value.(map[string]any)
	if !hookOK {
		t.Fatalf("expected command hook object for %s, got %#v", event, value)
	}
	command, commandOK := hook["command"].(string)
	if !commandOK || strings.TrimSpace(command) == "" {
		t.Fatalf("expected command for %s, got %#v", event, hook)
	}

	return command
}

func readCursorCommandHooks(t *testing.T, path string) map[string]string {
	t.Helper()

	config := decodeTestJSONObject(t, readTestFile(t, path, "reading Cursor replay hooks"), "Cursor replay hooks")
	hooks := requireTestHooks(t, config)
	commands := make(map[string]string, len(hooks))
	for event, value := range hooks {
		definitions, ok := value.([]any)
		if !ok || len(definitions) != 1 {
			t.Fatalf("expected one Cursor hook definition for %s, got %#v", event, value)
		}
		definition, ok := definitions[0].(map[string]any)
		if !ok {
			t.Fatalf("expected Cursor hook definition object for %s, got %#v", event, definitions[0])
		}
		command, ok := definition["command"].(string)
		if !ok || strings.TrimSpace(command) == "" {
			t.Fatalf("expected Cursor command for %s, got %#v", event, definition)
		}
		commands[event] = command
	}

	return commands
}

func readCopilotCommandHooks(t *testing.T, path string) map[string]string {
	t.Helper()

	config := decodeTestJSONObject(t, readTestFile(t, path, "reading Copilot replay hooks"), "Copilot replay hooks")
	hooks := requireTestHooks(t, config)
	commands := make(map[string]string, len(hooks))
	for event, value := range hooks {
		definitions, ok := value.([]any)
		if !ok || len(definitions) != 1 {
			t.Fatalf("expected one Copilot hook definition for %s, got %#v", event, value)
		}
		definition, ok := definitions[0].(map[string]any)
		if !ok {
			t.Fatalf("expected Copilot hook definition object for %s, got %#v", event, definitions[0])
		}
		command, ok := definition["command"].(string)
		if !ok || strings.TrimSpace(command) == "" {
			t.Fatalf("expected Copilot command for %s, got %#v", event, definition)
		}
		commands[event] = command
	}

	return commands
}

func readKimiManagedTextCommands(t *testing.T, path string) map[string][]string {
	t.Helper()

	text := string(readTestFile(t, path, "reading Kimi replay hooks"))
	commands := make(map[string][]string)
	currentEvent := ""
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "[[hooks]]" {
			currentEvent = ""
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch key {
		case "event":
			decoded, err := strconv.Unquote(strings.TrimSpace(value))
			if err != nil {
				t.Fatalf("unquoting Kimi event value %q: %v", value, err)
			}
			currentEvent = decoded
		case "command":
			decoded, err := strconv.Unquote(strings.TrimSpace(value))
			if err != nil {
				t.Fatalf("unquoting Kimi command value %q: %v", value, err)
			}
			if currentEvent == "" {
				t.Fatalf("Kimi command without event: %s", line)
			}
			commands[currentEvent] = append(commands[currentEvent], decoded)
		}
	}
	if len(commands) == 0 {
		t.Fatalf("expected Kimi hook commands in %s", text)
	}

	return commands
}

func readGoosePluginCommands(t *testing.T, dir string) map[string][]string {
	t.Helper()

	config := decodeTestJSONObject(
		t,
		readTestFile(t, filepath.Join(dir, "hooks", "hooks.json"), "reading Goose replay hooks"),
		"Goose replay hooks",
	)
	hooks := requireTestHooks(t, config)
	commands := make(map[string][]string, len(hooks))
	for event, value := range hooks {
		commands[event] = append(commands[event], jsonCommandsFromHookGroups(t, event, value)...)
	}

	return commands
}

func readAgyPluginCommands(t *testing.T, dir string) map[string]string {
	t.Helper()

	config := decodeTestJSONObject(t, readTestFile(t, filepath.Join(dir, "hooks.json"), "reading Agy replay hooks"), "Agy replay hooks")
	namespace, namespaceOK := config[agyPluginName].(map[string]any)
	if !namespaceOK {
		t.Fatalf("expected Agy namespace %q, got %#v", agyPluginName, config)
	}

	commands := make(map[string]string, len(namespace))
	for event, value := range namespace {
		items, itemsOK := value.([]any)
		if !itemsOK || len(items) != 1 {
			t.Fatalf("expected one Agy hook item for %s, got %#v", event, value)
		}
		commands[event] = agyCommandFromHookValue(t, event, items[0])
	}

	return commands
}

func agyCommandFromHookValue(t *testing.T, event string, value any) string {
	t.Helper()

	item, itemOK := value.(map[string]any)
	if !itemOK {
		t.Fatalf("expected Agy hook object for %s, got %#v", event, value)
	}
	if command, commandOK := item["command"].(string); commandOK {
		return command
	}
	hookValues, hooksOK := item["hooks"].([]any)
	if !hooksOK || len(hookValues) != 1 {
		t.Fatalf("expected Agy nested hook for %s, got %#v", event, item)
	}
	hook, hookOK := hookValues[0].(map[string]any)
	if !hookOK {
		t.Fatalf("expected Agy nested hook object for %s, got %#v", event, hookValues[0])
	}
	command, commandOK := hook["command"].(string)
	if !commandOK || strings.TrimSpace(command) == "" {
		t.Fatalf("expected Agy command for %s, got %#v", event, hook)
	}

	return command
}

func requireCommandContaining(t *testing.T, commands []string, needle string) string {
	t.Helper()

	for _, command := range commands {
		if strings.Contains(command, needle) {
			return command
		}
	}
	t.Fatalf("expected command containing %q, got %#v", needle, commands)

	return ""
}

func runOpenCodePluginEvent(t *testing.T, bun string, env replayEnv, pluginPath string, handler string, payload string) {
	t.Helper()

	script := fmt.Sprintf(`
import { AgentSessionsPlugin } from %s;

const plugin = await AgentSessionsPlugin({
  directory: "/repo/opencode",
  worktree: "/repo/opencode",
  project: { root: "/repo/opencode" },
});
const handler = %s;
const payload = JSON.parse(%s);
if (handler === "event") {
  await plugin.event({ event: payload });
} else {
  await plugin[handler](payload);
}
`, strconv.Quote(fileImportSpecifier(pluginPath)), strconv.Quote(handler), strconv.Quote(payload))
	runBunReplayScript(t, bun, env, script)
}

func runKiloPluginEvent(t *testing.T, bun string, env replayEnv, pluginPath string, handler string, payload string) {
	t.Helper()

	script := fmt.Sprintf(`
import plugin from %s;

const server = await plugin.server({
  directory: "/repo/kilo",
  worktree: "/repo/kilo",
  project: { root: "/repo/kilo" },
});
const handler = %s;
const payload = JSON.parse(%s);
if (handler === "event") {
  await server.event({ event: payload });
} else {
  await server[handler](payload);
}
`, strconv.Quote(fileImportSpecifier(pluginPath)), strconv.Quote(handler), strconv.Quote(payload))
	runBunReplayScript(t, bun, env, script)
}

func runPiExtensionEvent(t *testing.T, bun string, env replayEnv, pluginPath string, handler string, payload string) {
	t.Helper()

	script := fmt.Sprintf(`
import register from %s;

const handlers = new Map();
register({
  on(name, callback) {
    handlers.set(name, callback);
  },
});
const callback = handlers.get(%s);
if (!callback) {
  throw new Error("missing Pi handler");
}
const ctx = {
  cwd: "/repo/pi",
  sessionManager: {
    getSessionFile() {
      return "/tmp/pi-replay.jsonl";
    },
    getSessionId() {
      return "pi-replay";
    },
  },
};
await callback(JSON.parse(%s), ctx);
`, strconv.Quote(fileImportSpecifier(pluginPath)), strconv.Quote(handler), strconv.Quote(payload))
	runBunReplayScript(t, bun, env, script)
}

func runBunReplayScript(t *testing.T, bun string, env replayEnv, script string) {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "replay.mjs")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("writing Bun replay script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bun, scriptPath)
	cmd.Dir = env.root
	cmd.Env = replayEnvVars(env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("Bun replay timed out:\n%s\nstdout=%s\nstderr=%s", script, stdout.String(), stderr.String())
	}
	if err != nil {
		t.Fatalf("Bun replay failed: %v\n%s\nstdout=%s\nstderr=%s", err, script, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected Bun replay silence, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func fileImportSpecifier(path string) string {
	return (&url.URL{
		Scheme:      "file",
		Opaque:      "",
		User:        nil,
		Host:        "",
		Path:        path,
		RawPath:     "",
		OmitHost:    false,
		ForceQuery:  false,
		RawQuery:    "",
		Fragment:    "",
		RawFragment: "",
	}).String()
}

func requireCommands(t *testing.T, commands map[string]string, events []string) {
	t.Helper()

	for _, event := range events {
		if strings.TrimSpace(commands[event]) == "" {
			t.Fatalf("expected command for %s, got %#v", event, commands)
		}
	}
}

func requireCommandSlices(t *testing.T, commands map[string][]string, events []string) {
	t.Helper()

	for _, event := range events {
		if len(commands[event]) == 0 {
			t.Fatalf("expected command for %s, got %#v", event, commands)
		}
	}
}

func TestArtifactReplayReadersRequireExpectedEvents(t *testing.T) {
	env := newReplayEnv(t)

	t.Run("cursor", func(t *testing.T) {
		t.Setenv("HOME", env.home)
		result := installReplayHarness(t, registry.HarnessCursor, env)
		requireCommands(t, readCursorCommandHooks(t, result.Path), []string{
			"sessionStart",
			"beforeSubmitPrompt",
			"beforeShellExecution",
			"afterShellExecution",
			"stop",
			"sessionEnd",
		})
	})

	t.Run("copilot", func(t *testing.T) {
		t.Setenv("COPILOT_HOME", filepath.Join(env.home, ".copilot"))
		result := installReplayHarness(t, registry.HarnessCopilot, env)
		requireCommands(t, readCopilotCommandHooks(t, result.Path), []string{
			"sessionStart",
			"userPromptSubmitted",
			"preToolUse",
			"permissionRequest",
			"notification",
			"postToolUse",
			"postToolUseFailure",
			"agentStop",
			"sessionEnd",
		})
	})

	t.Run("cline", func(t *testing.T) {
		t.Setenv("CLINE_HOOKS_DIR", filepath.Join(env.home, ".cline", "hooks"))
		result := installReplayHarness(t, registry.HarnessCline, env)
		for _, name := range []string{
			"TaskStart",
			"TaskResume",
			"UserPromptSubmit",
			"PreToolUse",
			"PostToolUse",
			"TaskComplete",
			"TaskError",
			"TaskCancel",
			"SessionShutdown",
		} {
			path := filepath.Join(result.Path, name+".sh")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("expected Cline hook file %s: %v", path, err)
			}
		}
	})

	t.Run("kimi", func(t *testing.T) {
		t.Setenv("KIMI_CODE_HOME", filepath.Join(env.home, ".kimi-code"))
		result := installReplayHarness(t, registry.HarnessKimiCode, env)
		requireCommandSlices(t, readKimiManagedTextCommands(t, result.Path), []string{
			hookEventSessionStart,
			"UserPromptSubmit",
			"PermissionRequest",
			"PermissionResult",
			hookEventStop,
			"StopFailure",
			"Interrupt",
			"SessionEnd",
		})
	})

	t.Run("goose", func(t *testing.T) {
		t.Setenv("HOME", env.home)
		result := installReplayHarness(t, registry.HarnessGoose, env)
		requireCommandSlices(t, readGoosePluginCommands(t, result.Path), []string{
			hookEventSessionStart,
			"UserPromptSubmit",
			"PreToolUse",
			"PostToolUse",
			"PostToolUseFailure",
			"BeforeReadFile",
			"AfterFileEdit",
			"BeforeShellExecution",
			"AfterShellExecution",
			hookEventStop,
			"SessionEnd",
		})
	})

	t.Run("agy", func(t *testing.T) {
		t.Setenv("AGY_CONFIG_HOME", filepath.Join(env.home, ".gemini", "config"))
		result := installReplayHarness(t, registry.HarnessAgy, env)
		commands := readAgyPluginCommands(t, result.Path)
		requireCommands(t, commands, []string{"PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", hookEventStop})
	})

	t.Run("droid", func(t *testing.T) {
		t.Setenv("HOME", env.home)
		result := installReplayHarness(t, registry.HarnessDroid, env)
		requireCommandSlices(t, readJSONCommandHooks(t, result.Path), []string{
			hookEventSessionStart,
			"UserPromptSubmit",
			"PreToolUse",
			"PostToolUse",
			"Notification",
			hookEventStop,
			"SubagentStop",
			"PreCompact",
			"SessionEnd",
		})
	})
}

func TestArtifactReplayDoesNotUseUnexpectedHarnesses(t *testing.T) {
	all := []registry.Harness{
		registry.HarnessCodex,
		registry.HarnessClaude,
		registry.HarnessCursor,
		registry.HarnessCopilot,
		registry.HarnessCline,
		registry.HarnessKimiCode,
		registry.HarnessGrok,
		registry.HarnessGoose,
		registry.HarnessAgy,
		registry.HarnessDroid,
	}
	for _, harness := range all {
		if !slices.Contains(AllHarnesses, harness) {
			t.Fatalf("expected replayed harness %s to be installable", harness)
		}
	}
}
