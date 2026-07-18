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
	gooseCommand           = "goose"
	goosePluginName        = "agent-sessions-state"
	gooseMarkerFileName    = ".agent-sessions-managed"
	gooseIntegrationID     = "goose"
	gooseIntegrationSource = "goose-hook"
)

type gooseHarness struct {
	baseAdapter
}

type gooseHookSpec struct {
	event      string
	transition any
	matcher    string
}

func gooseAdapter() Adapter {
	return gooseHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessGoose, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (gooseHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{PluginDirectoryAction{Plan: PluginDirectoryInstallPlan{
			Dir:   filepath.Join(goosePluginsDir(), goosePluginName),
			Label: "goose plugin",
			Files: []PluginFileInstallSpec{
				{
					Name:    "plugin.json",
					Content: "",
					JSONContent: map[string]any{
						"name":        goosePluginName,
						"version":     IntegrationVersion,
						"description": ManagedMarker,
					},
				},
				{
					Name:        "hooks/hooks.json",
					Content:     "",
					JSONContent: gooseHookConfig(),
				},
				{
					Name:        "scripts/report.sh",
					Content:     gooseReportScript(binary),
					JSONContent: nil,
				},
				{
					Name:        gooseMarkerFileName,
					Content:     gooseMarkerContent(),
					JSONContent: nil,
				},
			},
			SnippetOrder:   []string{"plugin.json", "hooks/hooks.json", "scripts/report.sh", gooseMarkerFileName},
			MarkerFile:     gooseMarkerFileName,
			ImportManifest: nil,
			OpenClaw:       nil,
			Hermes:         nil,
		}}},
	}
}

func (gooseHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{gooseCommand, "session", resumeFlag, "--session-id", sessionID}
}

func (gooseHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return goosePayloadValidator(rawPayload)
}

func (gooseHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return goosePayloadDefaults(payload)
}

func gooseHookConfig() map[string]any {
	hooks := make(map[string]any)
	for _, spec := range gooseHookSpecs() {
		hooks[spec.event] = []any{gooseHookRule(spec)}
	}
	hooks[HookEventSessionStart] = []any{gooseSessionStartHookRule()}

	return map[string]any{"hooks": hooks}
}

func gooseHookSpecs() []gooseHookSpec {
	return []gooseHookSpec{
		{event: HookEventSessionStart, transition: registry.ActivityIdle, matcher: ""},
		{event: HookEventUserPromptSubmit, transition: registry.ActivityRunning, matcher: ""},
		{event: HookEventPreToolUse, transition: registry.ActivityRunning, matcher: ""},
		{event: HookEventPostToolUse, transition: registry.ActivityRunning, matcher: ""},
		{event: HookEventPostToolUseFailure, transition: registry.ActivityRunning, matcher: ""},
		{event: "BeforeReadFile", transition: registry.ActivityRunning, matcher: ""},
		{event: "AfterFileEdit", transition: registry.ActivityRunning, matcher: ""},
		{event: "BeforeShellExecution", transition: registry.ActivityRunning, matcher: ""},
		{event: "AfterShellExecution", transition: registry.ActivityRunning, matcher: ""},
		{event: HookEventStop, transition: registry.ActivityIdle, matcher: ""},
		{event: "SessionEnd", transition: registry.PresenceGone, matcher: ""},
	}
}

func gooseSessionStartHookRule() map[string]any {
	return map[string]any{
		"hooks": []any{
			gooseCommandHook(gooseHookSpec{event: HookEventSessionStart, transition: registry.ActivityIdle, matcher: ""}),
		},
	}
}

func gooseHookRule(spec gooseHookSpec) map[string]any {
	rule := map[string]any{
		"hooks": []any{
			gooseCommandHook(spec),
		},
	}
	if spec.matcher != "" {
		rule["matcher"] = spec.matcher
	}

	return rule
}

func gooseCommandHook(spec gooseHookSpec) map[string]any {
	return map[string]any{
		"type":    HookTypeCommand,
		"command": gooseHookCommand(spec),
		"timeout": float64(HookTimeoutSeconds),
	}
}

func gooseHookCommand(spec gooseHookSpec) string {
	return strings.Join([]string{
		"sh",
		"\"${PLUGIN_ROOT}/scripts/report.sh\"",
		ShellQuote(stringTransition(spec.transition)),
		ShellQuote(spec.event),
	}, " ")
}

func gooseReportScript(binary string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		"# " + ManagedMarker,
		"# AGENT_SESSIONS_INTEGRATION_ID=" + gooseIntegrationID,
		"# AGENT_SESSIONS_INTEGRATION_VERSION=" + strconv.Itoa(IntegrationVersion),
		"# AGENT_SESSIONS_SOURCE=" + gooseIntegrationSource,
		"transition=${1:-}",
		"event=${2:-}",
		`if [ -z "$transition" ] || [ -z "$event" ]; then`,
		"  exit 0",
		"fi",
		"if [ \"$transition\" = gone ]; then",
		"  " + ShellQuote(binary) + " report " + ShellQuote(string(registry.HarnessGoose)) + " --presence \"$transition\" --event \"$event\" --attribute " + ShellQuote("agent_sessions_integration_version="+strconv.Itoa(IntegrationVersion)) + " --attribute " + ShellQuote("agent_sessions_integration="+gooseIntegrationSource) + " --queue --raw-stdin-defaults-only --quiet >/dev/null 2>&1 || true",
		"else",
		"  " + ShellQuote(binary) + " report " + ShellQuote(string(registry.HarnessGoose)) + " --activity \"$transition\" --event \"$event\" --attribute " + ShellQuote("agent_sessions_integration_version="+strconv.Itoa(IntegrationVersion)) + " --attribute " + ShellQuote("agent_sessions_integration="+gooseIntegrationSource) + " --queue --raw-stdin-defaults-only --quiet >/dev/null 2>&1 || true",
		"fi",
		"",
	}, "\n")
}

func gooseMarkerContent() string {
	return strings.Join([]string{
		ManagedMarker,
		"AGENT_SESSIONS_INTEGRATION_ID=" + gooseIntegrationID,
		"AGENT_SESSIONS_INTEGRATION_VERSION=" + strconv.Itoa(IntegrationVersion),
		"AGENT_SESSIONS_SOURCE=" + gooseIntegrationSource,
		"",
	}, "\n")
}

func goosePayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "goose_event", payloadString(payload, "event"))
	addAttributeString(attributes, "goose_tool_name", payloadString(payload, "tool_name"))
	addAttributeString(attributes, "goose_matcher_context", payloadString(payload, "matcher_context"))

	cwd := payloadString(payload, "working_dir")

	return PayloadDefaults{
		SessionID:   payloadString(payload, "session_id"),
		SessionPath: "",
		CWD:         cwd,
		ProjectRoot: cwd,
		Event:       payloadString(payload, "event"),
		Attributes:  attributes,
	}
}

func goosePayloadValidator(rawPayload json.RawMessage) bool {
	payload, ok := payloadObject(rawPayload)
	if !ok {
		return false
	}

	return payloadString(payload, "session_id") != ""
}

func goosePluginsDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".agents", "plugins")
	}

	return filepath.Join(".agents", "plugins")
}
