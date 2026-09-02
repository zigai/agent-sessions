package catalog

import (
	"encoding/json"
	"testing"
)

func FuzzPayloadAdapters(f *testing.F) {
	for _, harness := range SupportedNames() {
		f.Add(harness, []byte(`{"session_id":"session","cwd":"/work","hook_event_name":"Stop","model":"model"}`))
	}
	f.Add("agy", []byte(`{"conversationId":"session","workspacePaths":["/work"],"toolCall":{"name":"ask_permission"}}`))
	f.Add("codex", []byte("not json"))

	f.Fuzz(func(t *testing.T, harnessName string, payload []byte) {
		harness, err := Normalize(harnessName)
		if err != nil {
			return
		}
		raw := json.RawMessage(payload)
		_ = PayloadCompatibleWithHarness(harness, raw)
		_ = DefaultsFromPayload(harness, raw)
	})
}
