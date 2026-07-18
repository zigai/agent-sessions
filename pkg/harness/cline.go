package harness

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	clineCommand           = "cline"
	clinePluginName        = "agent-sessions-state"
	clineMarkerFileName    = ".agent-sessions-managed"
	clineIntegrationSource = "cline-plugin"
)

//go:embed assets/cline/index.js.tmpl
var clinePluginTemplate string

type clineHarness struct {
	baseAdapter
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
	version := strconv.Itoa(IntegrationVersionFor(registry.HarnessCline))
	dir := filepath.Join(clineConfigDir(), "plugins", clinePluginName)

	return InstallPlan{Actions: []InstallAction{PluginDirectoryAction{Plan: PluginDirectoryInstallPlan{
		Dir:   dir,
		Label: "Cline plugin",
		Files: []PluginFileInstallSpec{
			{Name: "package.json", Content: "", JSONContent: map[string]any{
				"name": clinePluginName, "version": "0.0." + version, "private": true, "type": "module",
				"cline": map[string]any{"plugins": []any{map[string]any{
					"paths": []string{"./index.js"}, "capabilities": []string{"hooks"},
				}}},
			}},
			{Name: "index.js", Content: renderClinePlugin(binary, version), JSONContent: nil},
			{Name: clineMarkerFileName, Content: clineMarkerContent(version), JSONContent: nil},
		},
		SnippetOrder:   []string{"package.json", "index.js", clineMarkerFileName},
		MarkerFile:     clineMarkerFileName,
		ObsoleteFiles:  clineLegacyHookPaths(),
		ImportManifest: nil,
		OpenClaw:       nil,
		Hermes:         nil,
	}}}}
}

func (clineHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{clineCommand, "--id", sessionID}
}

// PayloadCompatible retains report-command compatibility with payloads from
// Cline's retired standalone-hook surface. New installations use AgentPlugin.
func (clineHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return clinePayloadValidator(rawPayload)
}

func (clineHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return clinePayloadDefaults(payload)
}

func renderClinePlugin(binary string, version string) string {
	replacer := strings.NewReplacer(
		"{{BINARY}}", strconv.Quote(binary),
		"{{INTEGRATION_VERSION}}", strconv.Quote(version),
	)

	return replacer.Replace(clinePluginTemplate)
}

func clineMarkerContent(version string) string {
	return fmt.Sprintf("%s\nAGENT_SESSIONS_INTEGRATION_ID=cline\nAGENT_SESSIONS_INTEGRATION_VERSION=%s\nAGENT_SESSIONS_SOURCE=%s\n", ManagedMarker, version, clineIntegrationSource)
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

	return filepath.Join(clineSessionDir(), sessionID, sessionID+".messages.json")
}

func clineConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("CLINE_DIR")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cline")
	}

	return ".cline"
}

func clineLegacyHooksDir() string {
	if value := strings.TrimSpace(os.Getenv("CLINE_HOOKS_DIR")); value != "" {
		return value
	}

	return filepath.Join(clineConfigDir(), "hooks")
}

func clineLegacyHookPaths() []string {
	names := []string{
		"TaskStart.sh",
		"TaskResume.sh",
		"UserPromptSubmit.sh",
		"PreToolUse.sh",
		"PostToolUse.sh",
		"TaskComplete.sh",
		"TaskCancel.sh",
		"TaskError.sh",
		"PreCompact.sh",
		"SessionShutdown.sh",
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(clineLegacyHooksDir(), name))
	}

	return paths
}

func clineSessionDir() string {
	if value := strings.TrimSpace(os.Getenv("CLINE_SESSION_DATA_DIR")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("CLINE_DATA_DIR")); value != "" {
		return filepath.Join(value, "sessions")
	}

	return filepath.Join(clineConfigDir(), "data", "sessions")
}
