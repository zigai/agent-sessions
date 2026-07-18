package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	cursorCommand           = "cursor-agent"
	cursorIntegrationSource = "cursor-hook"
)

type cursorHarness struct {
	baseAdapter
}

func cursorAdapter() Adapter {
	return cursorHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessCursor, EnvKeys{
			SessionID:   nil,
			SessionPath: []string{"CURSOR_TRANSCRIPT_PATH"},
			ProjectRoot: []string{"CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"},
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (cursorHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{CursorJSONHooksAction{Plan: CursorJSONHookInstallPlan{
			Path:        filepath.Join(cursorHome(), "hooks.json"),
			Source:      cursorIntegrationSource,
			Label:       "cursor hooks",
			ConfigLabel: "cursor config",
			Hooks: []CursorCommandHookInstallSpec{
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
		}}},
	}
}

func (cursorHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{cursorCommand, resumeFlag, sessionID}
}

func (cursorHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return payloadValidator[cursorHookPayload]()(rawPayload)
}

func (cursorHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return cursorPayloadDefaults(payload)
}

func cursorHookCommand[T hookTransition](binary string, transition T, event string, hookOutput string) string {
	report := RawStdinDefaultsReportHookCommand(binary, registry.HarnessCursor, transition, event, cursorIntegrationSource)

	return report + " >/dev/null 2>&1 || true; printf '%s\\n' " + ShellQuote(hookOutput)
}

func cursorPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "cursor_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "cursor_model", payloadString(payload, "model"))
	addAttributeString(attributes, "cursor_version", payloadString(payload, "cursor_version"))
	addAttributeString(attributes, "cursor_composer_mode", payloadString(payload, "composer_mode"))
	addAttributeString(attributes, "cursor_session_end_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "cursor_final_status", payloadString(payload, "final_status"))
	addAttributeString(attributes, "cursor_stop_status", payloadString(payload, "status"))
	addAttributeBool(attributes, "cursor_is_background_agent", payload, "is_background_agent")
	addAttributeBool(attributes, "cursor_sandbox", payload, "sandbox")

	projectRoot := firstPayloadString(payload, "workspace_roots")
	cwd := payloadString(payload, "cwd")
	if cwd == "" {
		cwd = projectRoot
	}

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "session_id", "conversation_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         cwd,
		ProjectRoot: projectRoot,
		Event:       payloadString(payload, "hook_event_name"),
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
