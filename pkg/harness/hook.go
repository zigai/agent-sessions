package harness

import (
	"encoding/json"

	"github.com/zigai/agent-sessions/pkg/registry"
)

type HookInvocation struct {
	Event      string
	RawPayload json.RawMessage
	Payload    map[string]any
	ParentArgs []string
}

type HookResult struct {
	Report   registry.Report
	ReportOK bool
	Response map[string]any
}

type HookAdapter struct {
	Event  func(payload map[string]any, explicitEvent string) string
	Handle func(invocation HookInvocation) HookResult
}

func HandleHook(
	harness registry.Harness,
	explicitEvent string,
	rawPayload json.RawMessage,
	payload map[string]any,
	parentArgs []string,
) (HookResult, bool) {
	adapter, ok := Find(harness)
	if !ok || adapter.Hook == nil || adapter.Hook.Handle == nil {
		var result HookResult

		return result, false
	}

	event := explicitEvent
	if adapter.Hook.Event != nil {
		event = adapter.Hook.Event(payload, explicitEvent)
	}

	result := adapter.Hook.Handle(HookInvocation{
		Event:      event,
		RawPayload: rawPayload,
		Payload:    payload,
		ParentArgs: parentArgs,
	})
	if result.Response == nil {
		result.Response = map[string]any{}
	}

	return result, true
}
