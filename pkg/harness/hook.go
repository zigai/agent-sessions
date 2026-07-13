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
	Report   registry.Observation
	ReportOK bool
	Response map[string]any
}

type HookAdapter interface {
	HandleHook(invocation HookInvocation) HookResult
}

func HandleHook(
	harness registry.Harness,
	explicitEvent string,
	rawPayload json.RawMessage,
	payload map[string]any,
	parentArgs []string,
) (HookResult, bool) {
	adapter, ok := Find(harness)
	if !ok {
		var result HookResult

		return result, false
	}
	hookAdapter, ok := adapter.(HookAdapter)
	if !ok {
		var result HookResult

		return result, false
	}

	result := hookAdapter.HandleHook(HookInvocation{
		Event:      explicitEvent,
		RawPayload: rawPayload,
		Payload:    payload,
		ParentArgs: parentArgs,
	})
	if result.Response == nil {
		result.Response = map[string]any{}
	}

	return result, true
}
