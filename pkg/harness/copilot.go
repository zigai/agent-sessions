package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	copilotHookFileName      = "agent-sessions.json"
	copilotIntegrationSource = "copilot-hook"
)

type copilotHarness struct {
	baseAdapter
}

type copilotHookSpec struct {
	event string
	state registry.State
}

func copilotAdapter() Adapter {
	return copilotHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessCopilot, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (copilotHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{RenderedFileAction{Plan: RenderedFileInstallPlan{
			Path:        filepath.Join(copilotHome(), "hooks", copilotHookFileName),
			Label:       "copilot hooks",
			ConfigLabel: "copilot hooks",
			Content:     "",
			JSONContent: copilotHookConfig(binary),
		}}},
	}
}

func (copilotHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return copilotPayloadValidator(rawPayload)
}

func (copilotHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return copilotPayloadDefaults(payload)
}

func copilotHookConfig(binary string) map[string]any {
	specs := []copilotHookSpec{
		{event: "sessionStart", state: registry.StateIdle},
		{event: "userPromptSubmitted", state: registry.StateRunning},
		{event: "preToolUse", state: registry.StateRunning},
		{event: "permissionRequest", state: registry.StateWaiting},
		{event: "notification", state: registry.StateWaiting},
		{event: "postToolUse", state: registry.StateRunning},
		{event: "postToolUseFailure", state: registry.StateRunning},
		{event: "agentStop", state: registry.StateIdle},
		{event: "sessionEnd", state: registry.StateExited},
	}

	hooks := make(map[string]any, len(specs))
	for _, spec := range specs {
		hooks[spec.event] = []any{copilotCommandHook(binary, spec)}
	}

	return map[string]any{
		"version": float64(1),
		"hooks":   hooks,
	}
}

func copilotCommandHook(binary string, spec copilotHookSpec) map[string]any {
	return map[string]any{
		"type":       HookTypeCommand,
		"command":    copilotHookCommand(binary, spec.state, spec.event),
		"timeoutSec": float64(HookTimeoutSeconds),
		"env": map[string]any{
			"AGENT_SESSIONS_MARKER": ManagedMarker,
		},
	}
}

func copilotHookCommand(binary string, state registry.State, event string) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"report",
		"--harness", ShellQuote(string(registry.HarnessCopilot)),
		"--state", ShellQuote(string(state)),
		"--event", ShellQuote(event),
		"--source", ShellQuote(copilotIntegrationSource),
		"--attribute", ShellQuote("agent_sessions_integration=" + copilotIntegrationSource),
		"--attribute", ShellQuote("copilot_hook_event=" + event),
		"--queue",
		"--raw-stdin-defaults-only",
		"--quiet",
		">/dev/null",
		"2>&1",
		"||",
		"true",
	}, " ")
}

func copilotPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "copilot_hook_event", payloadStringAny(payload, "hookEventName", "hook_event_name", "event"))
	addAttributeString(attributes, "copilot_tool_name", payloadStringAny(payload, "toolName", "tool_name"))
	addAttributeString(attributes, "copilot_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "copilot_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "copilot_stop_reason", payloadStringAny(payload, "stopReason", "stop_reason"))
	addAttributeString(attributes, "copilot_error", payloadString(payload, "error"))

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "sessionId", "session_id"),
		SessionPath: payloadStringAny(payload, "transcriptPath", "transcript_path"),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadStringAny(payload, "hookEventName", "hook_event_name", "event"),
		Attributes:  attributes,
	}
}

func copilotPayloadValidator(rawPayload json.RawMessage) bool {
	payload, ok := payloadObject(rawPayload)
	if !ok {
		return false
	}

	return payloadStringAny(payload, "sessionId", "session_id") != "" &&
		payloadString(payload, "cwd") != ""
}

func copilotHome() string {
	if value := strings.TrimSpace(os.Getenv("COPILOT_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".copilot")
	}

	return ".copilot"
}
