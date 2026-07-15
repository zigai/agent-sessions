package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	codexCommand           = "codex"
	codexIntegrationSource = "codex-hook"
)

type codexHarness struct {
	baseAdapter
}

func codexAdapter() Adapter {
	return codexHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessCodex, EnvKeys{
			SessionID:   []string{"CODEX_SESSION_ID"},
			SessionPath: []string{"CODEX_SESSION_PATH"},
			ProjectRoot: nil,
			PID:         []string{"CODEX_PID"},
			Event:       nil,
		}),
	}
}

func (codexHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{JSONCommandHooksAction{Plan: JSONCommandHookInstallPlan{
			Path:              filepath.Join(codexHome(), "hooks.json"),
			Source:            codexIntegrationSource,
			Label:             "codex hooks",
			ConfigLabel:       "codex config",
			StatusMessage:     "Recording agent session",
			OmitStatusMessage: false,
			Hooks: []CommandHookInstallSpec{
				{
					Event:   HookEventSessionStart,
					Matcher: "startup|resume|clear|compact",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityIdle, HookEventSessionStart, codexIntegrationSource),
				},
				{
					Event:   HookEventUserPromptSubmit,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityRunning, HookEventUserPromptSubmit, codexIntegrationSource),
				},
				{
					Event:   "PermissionRequest",
					Matcher: "*",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityWaiting, "PermissionRequest", codexIntegrationSource),
				},
				{
					Event:   HookEventPostToolUse,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityRunning, HookEventPostToolUse, codexIntegrationSource),
				},
				{
					Event:   "PreCompact",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityRunning, "PreCompact", codexIntegrationSource),
				},
				{
					Event:   "PostCompact",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityIdle, "PostCompact", codexIntegrationSource),
				},
				{
					Event:   "SubagentStart",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityRunning, "SubagentStart", codexIntegrationSource),
				},
				{
					Event:   "SubagentStop",
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityIdle, "SubagentStop", codexIntegrationSource),
				},
				{
					Event:   HookEventStop,
					Matcher: "",
					Command: ReportHookCommand(binary, registry.HarnessCodex, registry.ActivityIdle, HookEventStop, codexIntegrationSource),
				},
			},
		}}, ShimAction{}},
	}
}

func (codexHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{codexCommand, "resume", sessionID}
}

func (codexHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return payloadValidator[codexHookPayload]()(rawPayload)
}

func (codexHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return codexPayloadDefaults(payload)
}

func codexPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "codex_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "codex_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "codex_permission_mode", payloadString(payload, "permission_mode"))
	addAttributeString(attributes, "codex_model", payloadString(payload, "model"))
	addAttributeString(attributes, "codex_turn_id", payloadString(payload, "turn_id"))

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: payloadString(payload, "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func codexHome() string {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".codex")
	}

	return ".codex"
}
