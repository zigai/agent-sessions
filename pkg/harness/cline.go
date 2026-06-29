package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	clineCommand            = "cline"
	clineIntegrationID      = "cline"
	clineIntegrationVersion = "1"
	clineIntegrationSource  = "cline-hook"
)

type clineHarness struct {
	baseAdapter
}

type clineHookSpec struct {
	name  string
	state registry.State
}

func clineAdapter() Adapter {
	return clineHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessCline, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (clineHarness) InstallPlan(binary string) InstallPlan {
	specs := clineHookSpecs()
	files := make([]RenderedFileInstallSpec, 0, len(specs))
	order := make([]string, 0, len(specs))
	for _, spec := range specs {
		name := spec.name + ".sh"
		files = append(files, RenderedFileInstallSpec{
			Name:        name,
			Content:     clineHookScript(binary, spec),
			JSONContent: nil,
		})
		order = append(order, name)
	}

	return InstallPlan{
		Actions: []InstallAction{RenderedFilesAction{Plan: RenderedFilesInstallPlan{
			Dir:          clineHooksDir(),
			Label:        "cline hooks",
			ConfigLabel:  "cline hooks",
			Files:        files,
			SnippetOrder: order,
		}}},
	}
}

func (clineHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{clineCommand, "--id", sessionID}
}

func (clineHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return clinePayloadValidator(rawPayload)
}

func (clineHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return clinePayloadDefaults(payload)
}

func clineHookSpecs() []clineHookSpec {
	return []clineHookSpec{
		{name: "TaskStart", state: registry.StateIdle},
		{name: "TaskResume", state: registry.StateIdle},
		{name: HookEventUserPromptSubmit, state: registry.StateRunning},
		{name: HookEventPreToolUse, state: registry.StateRunning},
		{name: "PostToolUse", state: registry.StateRunning},
		{name: "TaskComplete", state: registry.StateIdle},
		{name: "TaskError", state: registry.StateIdle},
		{name: "TaskCancel", state: registry.StateIdle},
		{name: "SessionShutdown", state: registry.StateExited},
	}
}

func clineHookScript(binary string, spec clineHookSpec) string {
	return strings.Join([]string{
		"#!/bin/sh",
		"# " + ManagedMarker,
		"# AGENT_SESSIONS_INTEGRATION_ID=" + clineIntegrationID,
		"# AGENT_SESSIONS_INTEGRATION_VERSION=" + clineIntegrationVersion,
		"# AGENT_SESSIONS_SOURCE=" + clineIntegrationSource,
		clineHookCommand(binary, spec.state, spec.name) + " >/dev/null 2>&1 || true",
		"printf '%s\\n' '{}'",
		"",
	}, "\n")
}

func clineHookCommand(binary string, state registry.State, event string) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"report",
		"--harness", ShellQuote(string(registry.HarnessCline)),
		"--state", ShellQuote(string(state)),
		"--event", ShellQuote(event),
		"--source", ShellQuote(clineIntegrationSource),
		"--attribute", ShellQuote("agent_sessions_integration=" + clineIntegrationSource),
		"--attribute", ShellQuote("cline_hook_event=" + event),
		"--queue",
		"--raw-stdin-defaults-only",
		"--quiet",
	}, " ")
}

func clinePayloadDefaults(payload map[string]any) PayloadDefaults {
	sessionID := firstNonEmpty(nestedString(payload, "sessionContext", "rootSessionId"), payloadString(payload, "taskId"))
	projectRoot := firstNonEmpty(nestedString(payload, "workspaceInfo", "rootPath"), firstArrayString(payload, "workspaceRoots"))
	cwd := firstNonEmpty(payloadString(payload, "cwd"), projectRoot)

	attributes := make(map[string]string)
	addAttributeString(attributes, "cline_hook_event", payloadString(payload, "hookName"))
	addAttributeString(attributes, "cline_task_id", payloadString(payload, "taskId"))
	addAttributeString(attributes, "cline_version", payloadString(payload, "clineVersion"))
	addAttributeString(attributes, "cline_agent_id", payloadString(payload, "agent_id"))
	addAttributeString(attributes, "cline_parent_agent_id", payloadString(payload, "parent_agent_id"))
	addAttributeString(attributes, "cline_tool_name", firstNonEmpty(
		nestedString(payload, "tool_call", "name"),
		nestedString(payload, "tool_result", "name"),
	))
	addAttributeString(attributes, "cline_reason", payloadString(payload, "reason"))

	return PayloadDefaults{
		SessionID:   sessionID,
		SessionPath: clineSessionPath(sessionID),
		CWD:         cwd,
		ProjectRoot: projectRoot,
		Event:       payloadString(payload, "hookName"),
		Attributes:  attributes,
	}
}

func clinePayloadValidator(rawPayload json.RawMessage) bool {
	payload, ok := payloadObject(rawPayload)
	if !ok {
		return false
	}

	return firstNonEmpty(nestedString(payload, "sessionContext", "rootSessionId"), payloadString(payload, "taskId")) != "" &&
		payloadString(payload, "hookName") != ""
}

func clineSessionPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	return filepath.Join(clineDataDir(), "sessions", sessionID, sessionID+".messages.json")
}

func clineHooksDir() string {
	if value := strings.TrimSpace(os.Getenv("CLINE_HOOKS_DIR")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cline", "hooks")
	}

	return filepath.Join(".cline", "hooks")
}

func clineDataDir() string {
	if value := strings.TrimSpace(os.Getenv("CLINE_DATA_DIR")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cline", "data")
	}

	return filepath.Join(".cline", "data")
}
