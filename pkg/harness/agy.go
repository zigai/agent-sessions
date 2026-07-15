package harness

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	agyCommand                     = "agy"
	agyPluginName                  = "agent-sessions-state"
	agyMarkerFileName              = ".agent-sessions-managed"
	agyImportManifestName          = "import_manifest.json"
	agyImportSource                = "antigravity"
	agyImportComponent             = "hooks"
	agyIntegrationID               = "agy"
	agyHookSource                  = "agy-hook"
	agyHookAdditionalAttributeKeys = 3
)

type agyHarness struct {
	baseAdapter
}

func agyAdapter() Adapter {
	return agyHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessAgy, EnvKeys{
			SessionID:   nil,
			SessionPath: nil,
			ProjectRoot: nil,
			PID:         nil,
			Event:       nil,
		}),
	}
}

func (agyHarness) InstallPlan(binary string) InstallPlan {
	configDir := agyConfigDir()

	return InstallPlan{
		Actions: []InstallAction{PluginDirectoryAction{Plan: PluginDirectoryInstallPlan{
			Dir:   filepath.Join(configDir, "plugins", agyPluginName),
			Label: "agy plugin",
			Files: []PluginFileInstallSpec{
				{
					Name:        "plugin.json",
					Content:     "",
					JSONContent: map[string]any{"name": agyPluginName},
				},
				{
					Name:        "hooks.json",
					Content:     "",
					JSONContent: agyHookConfig(binary),
				},
				{
					Name:        agyMarkerFileName,
					Content:     agyMarkerContent(),
					JSONContent: nil,
				},
			},
			SnippetOrder: []string{"plugin.json", "hooks.json", agyMarkerFileName},
			MarkerFile:   agyMarkerFileName,
			ImportManifest: &ImportManifestInstallPlan{
				Path:       filepath.Join(configDir, agyImportManifestName),
				Name:       agyPluginName,
				Source:     agyImportSource,
				Components: []string{agyImportComponent},
			},
		}}, ShimAction{}},
	}
}

func (agyHarness) ResumeCommand(sessionID string, _ string) []string {
	return agyResumeCommand(sessionID)
}

func (agyHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return agyPayloadValidator(rawPayload)
}

func (agyHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return agyPayloadDefaults(payload)
}

func (agyHarness) HandleHook(invocation HookInvocation) HookResult {
	invocation.Event = agyHookEvent(invocation.Payload, invocation.Event)

	return agyHandleHook(invocation)
}

func agyHookConfig(binary string) map[string]any {
	return map[string]any{
		agyPluginName: map[string]any{
			"PreInvocation":      []any{agyHookHandler(binary, "PreInvocation")},
			"PostInvocation":     []any{agyHookHandler(binary, "PostInvocation")},
			HookEventPreToolUse:  []any{agyToolHookGroup(binary, HookEventPreToolUse)},
			HookEventPostToolUse: []any{agyToolHookGroup(binary, HookEventPostToolUse)},
			HookEventStop:        []any{agyHookHandler(binary, HookEventStop)},
		},
	}
}

func agyToolHookGroup(binary string, event string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{
			agyHookHandler(binary, event),
		},
	}
}

func agyHookHandler(binary string, event string) map[string]any {
	return map[string]any{
		"type":    HookTypeCommand,
		"command": agyHookCommand(binary, event),
		"timeout": float64(HookTimeoutSeconds),
	}
}

func agyHookCommand(binary string, event string) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"--json",
		"hook",
		string(registry.HarnessAgy),
		"--event", ShellQuote(event),
		"--queue",
	}, " ")
}

func agyMarkerContent() string {
	return strings.Join([]string{
		ManagedMarker,
		"AGENT_SESSIONS_INTEGRATION_ID=" + agyIntegrationID,
		"AGENT_SESSIONS_INTEGRATION_VERSION=" + strconv.Itoa(IntegrationVersionFor(registry.HarnessAgy)),
		"AGENT_SESSIONS_SOURCE=" + agyHookSource,
	}, "\n")
}

func agyConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("AGY_CONFIG_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".gemini", "antigravity-cli")
	}

	return filepath.Join(".gemini", "antigravity-cli")
}

func agyPayloadDefaults(payload map[string]any) PayloadDefaults {
	workspacePath := firstArrayString(payload, "workspacePaths", "workspace_paths")
	toolCWD := nestedString(payload, "toolCall", "args", "Cwd")
	cwd := firstNonEmpty(toolCWD, workspacePath)

	attributes := make(map[string]string)
	addAttributeString(attributes, "agy_hook_event", payloadStringAny(payload, "hookEventName", "hook_event_name", "event"))
	addAttributeString(attributes, "agy_tool_name", nestedString(payload, "toolCall", "name"))
	addAttributeString(attributes, "agy_termination_reason", payloadStringAny(payload, "terminationReason", "termination_reason"))
	addAttributeString(attributes, "agy_error", payloadString(payload, "error"))
	addAttributeString(attributes, "agy_fully_idle", payloadBoolString(payload, "fullyIdle", "fully_idle"))

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "conversationId", "conversation_id"),
		SessionPath: payloadStringAny(payload, "transcriptPath", "transcript_path"),
		CWD:         cwd,
		ProjectRoot: workspacePath,
		Event:       payloadStringAny(payload, "hookEventName", "hook_event_name", "event"),
		Attributes:  attributes,
	}
}

func agyHookEvent(payload map[string]any, explicitEvent string) string {
	return firstNonEmpty(explicitEvent, payloadStringAny(payload, "hookEventName", "hook_event_name", "event"))
}

func agyHandleHook(invocation HookInvocation) HookResult {
	report, ok := agyHookReport(invocation)

	return HookResult{
		Report:   report,
		ReportOK: ok,
		Response: agyHookResponse(invocation.Event),
	}
}

func agyHookReport(invocation HookInvocation) (registry.Observation, bool) {
	activity := agyActivityForHook(invocation.Event, invocation.Payload)
	if activity == nil {
		var observation registry.Observation
		return observation, false
	}

	defaults := agyPayloadDefaults(invocation.Payload)
	if defaults.SessionID == "" && defaults.SessionPath == "" {
		var observation registry.Observation
		return observation, false
	}

	return registry.Observation{ //nolint:exhaustruct // hook reports only native activity and catalog resume metadata
		Source:   registry.ObservationSourceNative,
		Evidence: registry.ObservationEvidenceNativeEvent,
		Harness:  registry.HarnessAgy,
		Identity: registry.ObservationIdentity{
			SessionID:   defaults.SessionID,
			SessionPath: defaults.SessionPath,
		},
		Activity:    activity,
		NativeEvent: invocation.Event,
		Catalog: &registry.CatalogMetadata{
			ResumeCommand: agyResumeCommand(defaults.SessionID),
			CWD:           defaults.CWD,
			ProjectRoot:   defaults.ProjectRoot,
			ProcessPID:    0,
			Current:       false,
		},
		Attributes: agyHookAttributes(defaults.Attributes, invocation.Event),
		RawPayload: invocation.RawPayload,
	}, true
}

func agyResumeCommand(sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	return []string{agyCommand, "--conversation", sessionID}
}

func agyActivityForHook(event string, payload map[string]any) *registry.Activity {
	var activity registry.Activity
	switch event {
	case "PreInvocation", "PostInvocation":
		activity = registry.ActivityRunning
	case HookEventPreToolUse:
		if isAgyInputWaitingTool(payload) {
			activity = registry.ActivityWaiting
		} else {
			activity = registry.ActivityRunning
		}
	case HookEventPostToolUse:
		if _, ok := payload["toolCall"].(map[string]any); !ok {
			return nil
		}
		activity = registry.ActivityRunning
	case "Stop":
		if payloadBool(payload, "fullyIdle", "fully_idle") {
			activity = registry.ActivityIdle
		} else {
			activity = registry.ActivityRunning
		}
	default:
		return nil
	}
	return &activity
}

func agyHookResponse(event string) map[string]any {
	switch event {
	case HookEventPreToolUse:
		return map[string]any{"decision": "allow"}
	case "Stop":
		return map[string]any{"decision": ""}
	default:
		return map[string]any{}
	}
}

func agyHookAttributes(defaultAttributes map[string]string, event string) map[string]string {
	attributes := make(map[string]string, len(defaultAttributes)+agyHookAdditionalAttributeKeys)
	maps.Copy(attributes, defaultAttributes)
	if event != "" {
		attributes["agy_hook_event"] = event
	}
	attributes["agent_sessions_integration"] = agyHookSource
	attributes["agent_sessions_integration_version"] = strconv.Itoa(IntegrationVersionFor(registry.HarnessAgy))
	return attributes
}

func isAgyInputWaitingTool(payload map[string]any) bool {
	switch agyToolName(payload) {
	case "ask_permission", "ask_question":
		return true
	default:
		return false
	}
}

func agyToolName(payload map[string]any) string {
	toolCall, ok := payload["toolCall"].(map[string]any)
	if !ok {
		return ""
	}

	name, ok := toolCall["name"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(name)
}

func nestedString(payload map[string]any, path ...string) string {
	if len(path) == 0 {
		return ""
	}

	var current any = payload
	for _, part := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = currentMap[part]
	}

	text, ok := current.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

func firstArrayString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			text, textOK := item.(string)
			if textOK && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}

	return ""
}

func payloadBoolString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return strconv.FormatBool(typed)
		case string:
			return strings.TrimSpace(typed)
		default:
			if typed != nil {
				return fmt.Sprint(typed)
			}
		}
	}

	return ""
}

func payloadBool(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(strings.TrimSpace(typed), "true")
		default:
			return strings.EqualFold(fmt.Sprint(typed), "true")
		}
	}

	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
