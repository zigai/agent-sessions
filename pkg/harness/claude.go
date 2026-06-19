package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	claudeCommand           = "claude"
	claudeIntegrationSource = "claude-hook"
)

type claudeHarness struct {
	baseAdapter
}

func claudeAdapter() Adapter {
	return claudeHarness{
		baseAdapter: newBaseAdapter(Definition{
			ID:           registry.HarnessClaude,
			Aliases:      []string{"claude-code", "claude_code"},
			ProcessNames: []string{claudeCommand},
			Env: EnvKeys{
				SessionID:   []string{"CLAUDE_SESSION_ID"},
				SessionPath: []string{"CLAUDE_SESSION_PATH"},
				ProjectRoot: nil,
				PID:         []string{"CLAUDE_PID"},
				Event:       nil,
			},
		}),
	}
}

func (claudeHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		JSONCommandHooks: &JSONCommandHookInstallPlan{
			Path:          filepath.Join(claudeConfigDir(), "settings.json"),
			Source:        claudeIntegrationSource,
			Label:         "claude hooks",
			ConfigLabel:   "claude config",
			StatusMessage: "",
			Hooks: []CommandHookInstallSpec{
				{
					Event:   HookEventSessionStart,
					Matcher: "startup|resume|clear|compact",
					Command: SelfRefreshCommand(binary, registry.HarnessClaude, true) + " " + ReportHookCommand(
						binary,
						registry.HarnessClaude,
						registry.StateIdle,
						HookEventSessionStart,
						claudeIntegrationSource,
					),
				},
				{
					Event:   "UserPromptSubmit",
					Matcher: "",
					Command: ReportHookCommand(
						binary,
						registry.HarnessClaude,
						registry.StateRunning,
						"UserPromptSubmit",
						claudeIntegrationSource,
					),
				},
				{
					Event:   "Notification",
					Matcher: "permission_prompt",
					Command: ReportHookCommand(
						binary,
						registry.HarnessClaude,
						registry.StateWaiting,
						"Notification",
						claudeIntegrationSource,
					),
				},
				{
					Event:   HookEventStop,
					Matcher: "",
					Command: ReportHookCommand(
						binary,
						registry.HarnessClaude,
						registry.StateIdle,
						HookEventStop,
						claudeIntegrationSource,
					),
				},
				{
					Event:   "SessionEnd",
					Matcher: "",
					Command: ReportHookCommand(
						binary,
						registry.HarnessClaude,
						registry.StateExited,
						"SessionEnd",
						claudeIntegrationSource,
					),
				},
			},
		},
		CursorJSONHooks:  nil,
		ManagedTextBlock: nil,
		RenderedFile:     nil,
		PluginDirectory:  nil,
		Shim:             nil,
	}
}

func (claudeHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{claudeCommand, "--resume", sessionID}
}

func (claudeHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return payloadValidator[claudeHookPayload]()(rawPayload)
}

func (claudeHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return claudePayloadDefaults(payload)
}

func claudePayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "claude_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "claude_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "claude_permission_mode", payloadString(payload, "permission_mode"))
	addAttributeString(attributes, "claude_model", payloadString(payload, "model"))
	addAttributeString(attributes, "claude_notification_type", payloadStringAny(payload, "notification_type", "type"))
	addAttributeString(attributes, "claude_session_end_reason", payloadString(payload, "reason"))

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func claudeConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".claude")
	}

	return ".claude"
}
