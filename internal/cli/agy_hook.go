package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/spf13/cobra"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

const agyHookSource = "agy-hook"

type agyHookOptions struct {
	event string
}

func (app *application) newAgyHookCommand() *cobra.Command {
	options := agyHookOptions{}

	cmd := &cobra.Command{
		Use:    "agy-hook",
		Short:  "Run the managed Antigravity CLI session hook",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runAgyHook(cmd.Context(), cmd.InOrStdin(), options)
		},
	}
	cmd.Flags().StringVar(&options.event, "event", "", "Antigravity hook event name")

	return cmd
}

func (app *application) runAgyHook(ctx context.Context, stdin io.Reader, options agyHookOptions) error {
	data, _ := io.ReadAll(stdin)
	rawPayload := rawPayloadFromHookBytes(data)
	payload := hookPayloadObject(rawPayload)
	event := firstNonEmptyString(options.event, payloadStringAnyLocal(payload, "hookEventName", "hook_event_name", "event"))

	reportAgyHook(ctx, app.registryStore(), event, rawPayload, payload)

	return app.writeJSON(agyHookResponse(event))
}

func reportAgyHook(
	ctx context.Context,
	store registry.Store,
	event string,
	rawPayload json.RawMessage,
	payload map[string]any,
) {
	state := agyStateForHook(event, payload)
	if state == "" {
		return
	}

	defaults := harnesspkg.DefaultsFromPayload(registry.HarnessAgy, rawPayload)
	if defaults.SessionID == "" && defaults.SessionPath == "" {
		return
	}

	attributes := make(map[string]string, len(defaults.Attributes)+2)
	maps.Copy(attributes, defaults.Attributes)
	if event != "" {
		attributes["agy_hook_event"] = event
	}
	attributes["agent_sessions_integration"] = agyHookSource

	tmux := registry.TmuxContext{}
	if collected, err := tmuxctx.Current(ctx); err == nil {
		tmux = collected
	}

	report := harnesspkg.WithResumeCommand(registry.Report{
		Harness:     registry.HarnessAgy,
		State:       state,
		SessionID:   defaults.SessionID,
		SessionPath: defaults.SessionPath,
		CWD:         defaults.CWD,
		ProjectRoot: defaults.ProjectRoot,
		Tmux:        tmux,
		Source:      agyHookSource,
		Confidence:  "hook",
		Event:       event,
		Attributes:  attributes,
		RawPayload:  rawPayload,
	})
	_, _ = store.Report(ctx, report)
}

func agyStateForHook(event string, payload map[string]any) registry.State {
	switch event {
	case "PreInvocation", "PostInvocation":
		return registry.StateRunning
	case "PreToolUse":
		if isAgyInputWaitingTool(payload) {
			return registry.StateWaiting
		}

		return registry.StateRunning
	case "PostToolUse":
		return registry.StateRunning
	case "Stop":
		if payloadBoolLocal(payload, "fullyIdle", "fully_idle") {
			return registry.StateIdle
		}

		return registry.StateRunning
	default:
		return ""
	}
}

func agyHookResponse(event string) map[string]any {
	switch event {
	case "PreToolUse":
		return map[string]any{"decision": "allow"}
	case "Stop":
		return map[string]any{"decision": ""}
	default:
		return map[string]any{}
	}
}

func rawPayloadFromHookBytes(data []byte) json.RawMessage {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}

	wrapped, err := json.Marshal(string(data))
	if err != nil {
		return nil
	}

	return json.RawMessage(wrapped)
}

func hookPayloadObject(rawPayload json.RawMessage) map[string]any {
	if len(rawPayload) == 0 {
		return map[string]any{}
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload == nil {
		return map[string]any{}
	}

	return payload
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

func payloadStringAnyLocal(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key].(string)
		if ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func payloadBoolLocal(payload map[string]any, keys ...string) bool {
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
