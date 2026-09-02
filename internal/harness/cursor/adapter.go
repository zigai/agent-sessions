package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/aht/internal/harness"
	"github.com/zigai/aht/pkg/registry"
)

const (
	cursorCommand           = "cursor-agent"
	cursorIntegrationSource = "cursor-hook"
)

type cursorHarness struct{ harness.BaseAdapter }

type hookPayload struct {
	SessionID      string   `json:"session_id"      validate:"required,notblank"`
	TranscriptPath *string  `json:"transcript_path" validate:"omitempty"`
	WorkspaceRoots []string `json:"workspace_roots" validate:"required,min=1,dive,notblank"`
	HookEventName  string   `json:"hook_event_name" validate:"required,notblank"`
	CursorVersion  string   `json:"cursor_version"  validate:"required,notblank"`
}

func New() harness.Adapter {
	return cursorHarness{BaseAdapter: harness.NewBaseAdapter(harness.Definition{
		ID:           registry.HarnessCursor,
		Aliases:      []string{"cursor-agent", "cursor_agent", "cursor-cli", "cursor_cli"},
		ProcessNames: []string{"cursor", "cursor-agent", "cursor-cli"},
		Env: harness.EnvKeys{
			SessionID:   nil,
			SessionPath: []string{"CURSOR_TRANSCRIPT_PATH"},
			ProjectRoot: []string{"CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"},
			PID:         nil,
			Event:       nil,
		},
		Capabilities: harness.Capabilities{
			SessionStart:      true,
			SessionEnd:        true,
			RunningIdle:       true,
			WaitingPermission: false,
			ProcessIdentity:   false,
			NativeCatalog:     false,
			TTYTmuxContext:    false,
		},
		IntegrationVersion: harness.IntegrationVersion,
	})}
}

func (cursorHarness) InstallPlan(binary string) harness.InstallPlan {
	return harness.InstallPlan{Actions: []harness.InstallAction{harness.CursorJSONHooksAction{Plan: harness.CursorJSONHookInstallPlan{
		Path:        filepath.Join(cursorHome(), "hooks.json"),
		Source:      cursorIntegrationSource,
		Label:       "cursor hooks",
		ConfigLabel: "cursor config",
		Hooks: []harness.CursorCommandHookInstallSpec{
			{
				Event:   "sessionStart",
				Command: cursorHookCommand(binary, registry.ActivityIdle, "sessionStart", "{}"),
			},
			{
				Event:   "beforeSubmitPrompt",
				Command: cursorHookCommand(binary, registry.ActivityRunning, "beforeSubmitPrompt", `{"continue":true}`),
			},
			{
				Event:   "stop",
				Command: cursorHookCommand(binary, registry.ActivityIdle, "stop", "{}"),
			},
			{
				Event:   "sessionEnd",
				Command: cursorHookCommand(binary, registry.PresenceGone, "sessionEnd", "{}"),
			},
		},
	}}}}
}

func (cursorHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{cursorCommand, harness.ResumeFlag, sessionID}
}

func (cursorHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return harness.PayloadValidator[hookPayload]()(rawPayload)
}

func (cursorHarness) PayloadDefaults(payload map[string]any) harness.PayloadDefaults {
	return cursorPayloadDefaults(payload)
}

func cursorHookCommand[T harness.Transition](binary string, transition T, event string, hookOutput string) string {
	report := harness.RawStdinDefaultsReportHookCommand(binary, registry.HarnessCursor, transition, event, cursorIntegrationSource)

	return report + " >/dev/null 2>&1 || true; printf '%s\\n' " + harness.ShellQuote(hookOutput)
}

func cursorPayloadDefaults(payload map[string]any) harness.PayloadDefaults {
	attributes := make(map[string]string)
	harness.AddAttributeString(attributes, "cursor_hook_event", harness.PayloadString(payload, "hook_event_name"))
	harness.AddAttributeString(attributes, "cursor_start_source", harness.PayloadString(payload, "source"))
	harness.AddAttributeString(attributes, "cursor_model", harness.PayloadString(payload, "model"))
	harness.AddAttributeString(attributes, "cursor_version", harness.PayloadString(payload, "cursor_version"))
	harness.AddAttributeString(attributes, "cursor_composer_mode", harness.PayloadString(payload, "composer_mode"))
	harness.AddAttributeString(attributes, "cursor_session_end_reason", harness.PayloadString(payload, "reason"))
	harness.AddAttributeString(attributes, "cursor_final_status", harness.PayloadString(payload, "final_status"))
	harness.AddAttributeString(attributes, "cursor_stop_status", harness.PayloadString(payload, "status"))
	addAttributeBool(attributes, "cursor_is_background_agent", payload, "is_background_agent")
	addAttributeBool(attributes, "cursor_sandbox", payload, "sandbox")

	projectRoot := firstPayloadString(payload, "workspace_roots")
	cwd := harness.PayloadString(payload, "cwd")
	if cwd == "" {
		cwd = projectRoot
	}

	return harness.PayloadDefaults{
		SessionID:   harness.PayloadStringAny(payload, "session_id", "conversation_id"),
		SessionPath: harness.PayloadString(payload, "transcript_path"),
		CWD:         cwd,
		ProjectRoot: projectRoot,
		Event:       harness.PayloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func firstPayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return ""
	}

	text, ok := items[0].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

func addAttributeBool(attributes map[string]string, attributeKey string, payload map[string]any, payloadKey string) {
	value, ok := payload[payloadKey].(bool)
	if !ok {
		return
	}

	attributes[attributeKey] = strconv.FormatBool(value)
}

func cursorHome() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cursor")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".cursor")
	}

	return ".cursor"
}
