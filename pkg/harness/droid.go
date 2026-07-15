package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	droidCommand           = "droid"
	droidIntegrationSource = "droid-hook"
)

type droidHarness struct {
	baseAdapter
}

func droidAdapter() Adapter {
	return droidHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessDroid, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: []string{"FACTORY_PROJECT_DIR"},
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (droidHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{JSONCommandHooksAction{Plan: JSONCommandHookInstallPlan{
			Path:              filepath.Join(droidConfigDir(), "hooks.json"),
			Source:            droidIntegrationSource,
			Label:             "droid hooks",
			ConfigLabel:       "factory config",
			StatusMessage:     "",
			OmitStatusMessage: true,
			Hooks: []CommandHookInstallSpec{
				{
					Event:   HookEventSessionStart,
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityIdle, HookEventSessionStart),
				},
				{
					Event:   HookEventUserPromptSubmit,
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityRunning, HookEventUserPromptSubmit),
				},
				{
					Event:   HookEventPreToolUse,
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityRunning, HookEventPreToolUse),
				},
				{
					Event:   HookEventPostToolUse,
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityRunning, HookEventPostToolUse),
				},
				{
					Event:   "Notification",
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityWaiting, "Notification"),
				},
				{
					Event:   HookEventStop,
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityIdle, HookEventStop),
				},
				{
					Event:   "SubagentStop",
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityIdle, "SubagentStop"),
				},
				{
					Event:   "PreCompact",
					Matcher: "",
					Command: droidHookCommand(binary, registry.ActivityRunning, "PreCompact"),
				},
				{
					Event:   "SessionEnd",
					Matcher: "",
					Command: droidHookCommand(binary, registry.PresenceGone, "SessionEnd"),
				},
			},
		}}},
	}
}

func (droidHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{droidCommand, resumeFlag, sessionID}
}

func (droidHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return droidPayloadValidator(rawPayload)
}

func (droidHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return droidPayloadDefaults(payload)
}

func droidHookCommand(binary string, transition any, event string) string {
	return RawStdinDefaultsReportHookCommand(binary, registry.HarnessDroid, transition, event, droidIntegrationSource)
}

func droidPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "droid_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "droid_tool_name", payloadString(payload, "tool_name"))
	addAttributeString(attributes, "droid_permission_mode", payloadString(payload, "permission_mode"))
	addAttributeString(attributes, "droid_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "droid_source", payloadString(payload, "source"))
	addAttributeString(attributes, "droid_stop_hook_active", payloadBoolString(payload, "stop_hook_active"))

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func droidPayloadValidator(rawPayload json.RawMessage) bool {
	payload, ok := payloadObject(rawPayload)
	if !ok {
		return false
	}

	return payloadString(payload, "session_id") != "" &&
		payloadString(payload, "cwd") != "" &&
		payloadString(payload, "hook_event_name") != ""
}

func droidConfigDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".factory")
	}

	return ".factory"
}
