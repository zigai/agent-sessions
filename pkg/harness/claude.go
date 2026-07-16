package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
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
		baseAdapter: newMetadataAdapter(registry.HarnessClaude, EnvKeys{
			SessionID:   []string{"CLAUDE_SESSION_ID"},
			SessionPath: []string{"CLAUDE_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"CLAUDE_PID"},
			Event:       nil,
		}),
	}
}

func (claudeHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{JSONCommandHooksAction{Plan: JSONCommandHookInstallPlan{
			Path:              filepath.Join(claudeConfigDir(), "settings.json"),
			Source:            claudeIntegrationSource,
			Label:             "claude hooks",
			ConfigLabel:       "claude config",
			StatusMessage:     "",
			OmitStatusMessage: false,
			Hooks: []CommandHookInstallSpec{
				{
					Event:   HookEventSessionStart,
					Matcher: "startup|resume|clear|compact",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, HookEventSessionStart, claudeIntegrationSource),
				},
				{
					Event:   HookEventUserPromptSubmit,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, HookEventUserPromptSubmit, claudeIntegrationSource),
				},
				{
					Event:   HookEventPreToolUse,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, HookEventPreToolUse, claudeIntegrationSource),
				},
				{
					Event:   HookEventPostToolUse,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, HookEventPostToolUse, claudeIntegrationSource),
				},
				{
					Event:   HookEventPostToolUseFailure,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, HookEventPostToolUseFailure, claudeIntegrationSource),
				},
				{
					Event:   "PermissionRequest",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityWaiting, "PermissionRequest", claudeIntegrationSource),
				},
				{
					Event:   "PermissionDenied",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, "PermissionDenied", claudeIntegrationSource),
				},
				{
					Event:   "Notification",
					Matcher: "permission_prompt",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityWaiting, "Notification", claudeIntegrationSource),
				},
				{
					Event:   "SubagentStart",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, "SubagentStart", claudeIntegrationSource),
				},
				{
					Event:   "SubagentStop",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, "SubagentStop", claudeIntegrationSource),
				},
				{
					Event:   "PreCompact",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityRunning, "PreCompact", claudeIntegrationSource),
				},
				{
					Event:   "PostCompact",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, "PostCompact", claudeIntegrationSource),
				},
				{
					Event:   HookEventStop,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, HookEventStop, claudeIntegrationSource),
				},
				{
					Event:   "StopFailure",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.ActivityIdle, "StopFailure", claudeIntegrationSource),
				},
				{
					Event:   "SessionEnd",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessClaude, registry.PresenceGone, "SessionEnd", claudeIntegrationSource),
				},
			},
		}}},
	}
}

func (claudeHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{claudeCommand, resumeFlag, sessionID}
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
