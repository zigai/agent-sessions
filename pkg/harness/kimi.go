package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const kimiCommand = "kimi"

const (
	kimiCodeSessionIndexInitialBufferSize = 64 * 1024
	kimiCodeSessionIndexMaxBufferSize     = 1024 * 1024
)

func kimiCodeAdapter() Adapter {
	return Adapter{
		ID:           registry.HarnessKimiCode,
		Aliases:      []string{"kimi", "kimi_code", "kimicode"},
		ProcessNames: []string{kimiCommand},
		Env:          emptyEnvKeys(),
		Installable:  true,
		ResumeCommand: func(sessionID string, _ string) []string {
			if sessionID == "" {
				return nil
			}

			return []string{kimiCommand, sessionFlag, sessionID}
		},
		PayloadDefaults: kimiCodePayloadDefaults,
	}
}

func kimiCodePayloadDefaults(payload map[string]any) PayloadDefaults {
	sessionID := payloadString(payload, "session_id")
	attributes := make(map[string]string)
	addAttributeString(attributes, "kimi_code_hook_event", payloadString(payload, "hook_event_name"))
	addAttributeString(attributes, "kimi_code_start_source", payloadString(payload, "source"))
	addAttributeString(attributes, "kimi_code_tool_name", payloadString(payload, "tool_name"))
	addAttributeString(attributes, "kimi_code_turn_id", payloadScalarString(payload, "turn_id"))
	addAttributeString(attributes, "kimi_code_decision", payloadString(payload, "decision"))
	addAttributeString(attributes, "kimi_code_reason", payloadString(payload, "reason"))
	addAttributeString(attributes, "kimi_code_notification_type", payloadStringAny(payload, "notification_type", "type"))

	return PayloadDefaults{
		SessionID:   sessionID,
		SessionPath: kimiCodeSessionPath(sessionID),
		CWD:         payloadString(payload, "cwd"),
		ProjectRoot: "",
		Event:       payloadString(payload, "hook_event_name"),
		Attributes:  attributes,
	}
}

func payloadScalarString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func kimiCodeSessionPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	file, err := os.Open(filepath.Join(kimiCodeHome(), "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, kimiCodeSessionIndexInitialBufferSize), kimiCodeSessionIndexMaxBufferSize)
	for scanner.Scan() {
		var entry map[string]any
		if unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &entry); unmarshalErr != nil {
			continue
		}
		sessionDir := payloadString(entry, "sessionDir")
		if payloadString(entry, "sessionId") == sessionID && sessionDir != "" {
			return sessionDir
		}
	}

	return ""
}

func kimiCodeHome() string {
	if value := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME")); value != "" {
		return value
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".kimi-code")
	}

	return ".kimi-code"
}
