package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	grokHookFileName      = "agent-sessions-state.json"
	grokIntegrationSource = "grok-hook"
)

type grokHarness struct {
	baseAdapter
}

func grokAdapter() Adapter {
	return grokHarness{
		baseAdapter: newMetadataAdapter(registry.HarnessGrok, EnvKeys{
			SessionID:   []string{"GROK_SESSION_ID"},
			SessionPath: nil,
			ProjectRoot: []string{"GROK_WORKSPACE_ROOT"},
			PID:         nil,
			Event:       []string{"GROK_HOOK_EVENT"},
		}),
	}
}

func (grokHarness) InstallPlan(binary string) InstallPlan {
	return InstallPlan{
		Actions: []InstallAction{RenderedFileAction{Plan: RenderedFileInstallPlan{
			Path:        filepath.Join(grokHome(), "hooks", grokHookFileName),
			Label:       "grok hooks",
			ConfigLabel: "grok hooks",
			Content:     "",
			JSONContent: grokHookConfig(binary),
		}}},
	}
}

func (grokHarness) ResumeCommand(sessionID string, _ string) []string {
	if sessionID == "" {
		return nil
	}

	return []string{"grok", resumeFlag, sessionID}
}

func (grokHarness) PayloadCompatible(rawPayload json.RawMessage) bool {
	return grokPayloadValidator(rawPayload)
}

func (grokHarness) PayloadDefaults(payload map[string]any) PayloadDefaults {
	return grokPayloadDefaults(payload)
}

type grokHookSpec struct {
	event   string
	matcher string
	command string
}

func grokHookConfig(binary string) map[string]any {
	specs := []grokHookSpec{
		{
			event:   HookEventSessionStart,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, HookEventSessionStart),
		},
		{
			event:   HookEventUserPromptSubmit,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, HookEventUserPromptSubmit),
		},
		{
			event:   HookEventPreToolUse,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, HookEventPreToolUse),
		},
		{
			event:   HookEventPostToolUse,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, HookEventPostToolUse),
		},
		{
			event:   HookEventPostToolUseFailure,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, HookEventPostToolUseFailure),
		},
		{
			event:   "PermissionDenied",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, "PermissionDenied"),
		},
		{
			event:   "SubagentStart",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, "SubagentStart"),
		},
		{
			event:   "SubagentStop",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, "SubagentStop"),
		},
		{
			event:   "PreCompact",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityRunning, "PreCompact"),
		},
		{
			event:   "PostCompact",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, "PostCompact"),
		},
		{
			event:   HookEventStop,
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, HookEventStop),
		},
		{
			event:   "StopFailure",
			matcher: "",
			command: grokHookCommand(binary, registry.ActivityIdle, "StopFailure"),
		},
		{
			event:   "SessionEnd",
			matcher: "",
			command: grokHookCommand(binary, registry.PresenceGone, "SessionEnd"),
		},
	}

	hooks := make(map[string]any)
	for _, spec := range specs {
		existing, ok := hooks[spec.event].([]any)
		if !ok {
			existing = nil
		}
		hooks[spec.event] = append(existing, grokCommandHookGroup(spec.command, spec.matcher))
	}

	return map[string]any{"hooks": hooks}
}

func grokCommandHookGroup(command string, matcher string) map[string]any {
	group := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":          HookTypeCommand,
				"command":       command,
				"timeout":       float64(HookTimeoutSeconds),
				"statusMessage": ManagedMarker,
			},
		},
	}
	if matcher != "" {
		group["matcher"] = matcher
	}

	return group
}

func grokHookCommand(binary string, transition any, event string) string {
	return ReportHookCommand(binary, registry.HarnessGrok, transition, event, grokIntegrationSource)
}

func grokPayloadDefaults(payload map[string]any) PayloadDefaults {
	attributes := make(map[string]string)
	addAttributeString(attributes, "grok_hook_event", payloadStringAny(payload, "hookEventName", "hook_event_name"))
	addAttributeString(attributes, "grok_tool_name", payloadStringAny(payload, "toolName", "tool_name"))
	addAttributeString(attributes, "grok_notification_type", payloadStringAny(
		payload,
		"notificationType",
		"notification_type",
		"type",
	))

	return PayloadDefaults{
		SessionID:   payloadStringAny(payload, "sessionId", "session_id"),
		SessionPath: "",
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: payloadStringAny(payload, "workspaceRoot", "workspace_root"),
		Event:       payloadStringAny(payload, "hookEventName", "hook_event_name"),
		Attributes:  attributes,
	}
}

func grokHome() string {
	if value := strings.TrimSpace(os.Getenv("GROK_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".grok")
	}

	return ".grok"
}
