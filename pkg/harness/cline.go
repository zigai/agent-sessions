package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	clineCommand           = "cline"
	clineIntegrationID     = "cline"
	clineIntegrationSource = "cline-hook"
)

type clineHarness struct {
	baseAdapter
}

type clineHookSpec struct {
	name       string
	transition any
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
		{name: "TaskStart", transition: registry.ActivityIdle},
		{name: "TaskResume", transition: registry.ActivityIdle},
		{name: HookEventUserPromptSubmit, transition: registry.ActivityRunning},
		{name: HookEventPreToolUse, transition: registry.ActivityRunning},
		{name: "PostToolUse", transition: registry.ActivityRunning},
		{name: "TaskComplete", transition: registry.ActivityIdle},
		{name: "TaskCancel", transition: registry.ActivityIdle},
	}
}

func clineHookScript(binary string, spec clineHookSpec) string {
	return strings.Join([]string{
		"#!/bin/sh",
		"# " + ManagedMarker,
		"# AGENT_SESSIONS_INTEGRATION_ID=" + clineIntegrationID,
		"# AGENT_SESSIONS_INTEGRATION_VERSION=" + strconv.Itoa(IntegrationVersion),
		"# AGENT_SESSIONS_SOURCE=" + clineIntegrationSource,
		clineHookCommand(binary, spec.transition, spec.name) + " >/dev/null 2>&1 || true",
		"printf '%s\\n' '{}'",
		"",
	}, "\n")
}

func clineHookCommand(binary string, transition any, event string) string {
	return reportHookCommand(binary, registry.HarnessCline, transition, event, clineIntegrationSource, "--raw-stdin-defaults-only") +
		" --attribute " + ShellQuote("cline_hook_event="+event)
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
