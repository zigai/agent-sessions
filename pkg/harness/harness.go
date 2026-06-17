package harness

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

type EnvField string

const (
	EnvSessionID   EnvField = "session_id"
	EnvSessionPath EnvField = "session_path"
	EnvProjectRoot EnvField = "project_root"
	EnvPID         EnvField = "pid"
	EnvEvent       EnvField = "event"
)

type EnvKeys struct {
	SessionID   []string
	SessionPath []string
	ProjectRoot []string
	PID         []string
	Event       []string
}

type PayloadDefaults struct {
	SessionID   string
	SessionPath string
	CWD         string
	ProjectRoot string
	Event       string
	Attributes  map[string]string
}

type Adapter struct {
	ID              registry.Harness
	Aliases         []string
	ProcessNames    []string
	Env             EnvKeys
	Installable     bool
	ResumeCommand   func(sessionID string, sessionPath string) []string
	PayloadDefaults func(payload map[string]any) PayloadDefaults
}

func All() []Adapter {
	result := make([]Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		result = append(result, cloneAdapter(adapter))
	}

	return result
}

func Find(harness registry.Harness) (Adapter, bool) {
	for _, adapter := range adapters {
		if adapter.ID == harness {
			return cloneAdapter(adapter), true
		}
	}

	return emptyAdapter(), false
}

func Normalize(value string) (registry.Harness, error) {
	normalized := normalizeToken(value)
	for _, adapter := range adapters {
		if normalized == normalizeToken(string(adapter.ID)) {
			return adapter.ID, nil
		}
		for _, alias := range adapter.Aliases {
			if normalized == normalizeToken(alias) {
				return adapter.ID, nil
			}
		}
	}

	return "", fmt.Errorf("%w: %q", registry.ErrUnknownHarness, value)
}

func SupportedNames() []string {
	names := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		names = append(names, string(adapter.ID))
	}

	return names
}

func EnvNames(field EnvField) []string {
	names := genericEnvNames(field)
	for _, adapter := range adapters {
		names = appendUnique(names, envNamesForField(adapter.Env, field)...)
	}

	return names
}

func FromCommand(command string) (registry.Harness, bool) {
	normalized := normalizeToken(filepath.Base(command))
	for _, adapter := range adapters {
		for _, processName := range adapter.ProcessNames {
			if normalized == normalizeToken(processName) {
				return adapter.ID, true
			}
		}
	}

	return "", false
}

func DefaultsFromPayload(harness registry.Harness, rawPayload json.RawMessage) PayloadDefaults {
	if len(rawPayload) == 0 {
		return emptyPayloadDefaults()
	}

	adapter, ok := Find(harness)
	if !ok || adapter.PayloadDefaults == nil {
		return emptyPayloadDefaults()
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return emptyPayloadDefaults()
	}

	return adapter.PayloadDefaults(payload)
}

func ResumeCommandFor(harness registry.Harness, sessionID string, sessionPath string) []string {
	adapter, ok := Find(harness)
	if !ok || adapter.ResumeCommand == nil {
		return nil
	}

	return adapter.ResumeCommand(sessionID, sessionPath)
}

func WithResumeCommand(report registry.Report) registry.Report {
	if len(report.ResumeCommand) == 0 {
		report.ResumeCommand = ResumeCommandFor(report.Harness, report.SessionID, report.SessionPath)
	}

	return report
}

func cloneAdapter(adapter Adapter) Adapter {
	adapter.Aliases = cloneStrings(adapter.Aliases)
	adapter.ProcessNames = cloneStrings(adapter.ProcessNames)
	adapter.Env = cloneEnvKeys(adapter.Env)

	return adapter
}

func emptyAdapter() Adapter {
	return Adapter{
		ID:              "",
		Aliases:         nil,
		ProcessNames:    nil,
		Env:             emptyEnvKeys(),
		Installable:     false,
		ResumeCommand:   nil,
		PayloadDefaults: nil,
	}
}

func emptyPayloadDefaults() PayloadDefaults {
	return PayloadDefaults{
		SessionID:   "",
		SessionPath: "",
		CWD:         "",
		ProjectRoot: "",
		Event:       "",
		Attributes:  nil,
	}
}

func emptyEnvKeys() EnvKeys {
	return EnvKeys{
		SessionID:   nil,
		SessionPath: nil,
		ProjectRoot: nil,
		PID:         nil,
		Event:       nil,
	}
}

func cloneEnvKeys(keys EnvKeys) EnvKeys {
	return EnvKeys{
		SessionID:   cloneStrings(keys.SessionID),
		SessionPath: cloneStrings(keys.SessionPath),
		ProjectRoot: cloneStrings(keys.ProjectRoot),
		PID:         cloneStrings(keys.PID),
		Event:       cloneStrings(keys.Event),
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func genericEnvNames(field EnvField) []string {
	switch field {
	case EnvSessionID:
		return []string{"AGENT_SESSIONS_SESSION_ID", "AGENT_SESSION_ID"}
	case EnvSessionPath:
		return []string{"AGENT_SESSIONS_SESSION_PATH", "AGENT_SESSION_PATH"}
	case EnvProjectRoot:
		return []string{"AGENT_SESSIONS_PROJECT_ROOT", "PROJECT_ROOT"}
	case EnvPID:
		return []string{"AGENT_SESSIONS_PID", "AGENT_PID"}
	case EnvEvent:
		return []string{"AGENT_SESSIONS_EVENT", "AGENT_EVENT"}
	default:
		return nil
	}
}

func envNamesForField(keys EnvKeys, field EnvField) []string {
	switch field {
	case EnvSessionID:
		return keys.SessionID
	case EnvSessionPath:
		return keys.SessionPath
	case EnvProjectRoot:
		return keys.ProjectRoot
	case EnvPID:
		return keys.PID
	case EnvEvent:
		return keys.Event
	default:
		return nil
	}
}

func appendUnique(values []string, next ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(next))
	for _, value := range values {
		seen[value] = struct{}{}
	}

	for _, value := range next {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}

	return values
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
