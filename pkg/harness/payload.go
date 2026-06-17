package harness

import "strings"

func payloadStringAny(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := payloadString(payload, key); value != "" {
			return value
		}
	}

	return ""
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	text, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

func addAttributeString(attributes map[string]string, key string, value string) {
	if value == "" {
		return
	}

	attributes[key] = value
}
