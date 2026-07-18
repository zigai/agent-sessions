package harness

import (
	"encoding/json"
	"fmt"

	"github.com/zigai/agent-sessions/v2/pkg/harnessmeta"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
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

type Capabilities struct {
	SessionStart      bool
	SessionEnd        bool
	RunningIdle       bool
	WaitingPermission bool
	ProcessIdentity   bool
	NativeCatalog     bool
	TTYTmuxContext    bool
}

type Definition struct {
	ID           registry.Harness
	Aliases      []string
	ProcessNames []string
	Env          EnvKeys
	Capabilities Capabilities
}

const IntegrationVersion = 3

// IntegrationVersionFor returns the managed artifact generation for a harness.
func IntegrationVersionFor(id registry.Harness) int {
	if id == registry.HarnessAgy {
		return IntegrationVersion + 1
	}
	return IntegrationVersion
}

//nolint:cyclop,exhaustruct // capabilities are a documented per-harness matrix
func capabilitiesFor(id registry.Harness) Capabilities {
	capabilities := Capabilities{SessionStart: false, SessionEnd: false, RunningIdle: false, WaitingPermission: false, ProcessIdentity: false, NativeCatalog: false, TTYTmuxContext: false}
	switch id {
	case registry.HarnessClaude:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.WaitingPermission, capabilities.NativeCatalog = true, true, true, true, true
	case registry.HarnessCodex:
		capabilities.SessionStart, capabilities.RunningIdle, capabilities.WaitingPermission = true, true, true
	case registry.HarnessCursor:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle = true, true, true
	case registry.HarnessCopilot:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.WaitingPermission = true, true, true, true
	case registry.HarnessCline:
		capabilities.RunningIdle = true
	case registry.HarnessKimiCode:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.WaitingPermission, capabilities.NativeCatalog = true, true, true, true, true
	case registry.HarnessGrok:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle = true, true, true
	case registry.HarnessGoose:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.NativeCatalog = true, true, true, true
	case registry.HarnessPi:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.NativeCatalog = true, true, true, true
	case registry.HarnessOmp:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.WaitingPermission, capabilities.NativeCatalog = true, true, true, true, true
	case registry.HarnessOpenCode:
		capabilities.SessionStart, capabilities.RunningIdle, capabilities.WaitingPermission, capabilities.NativeCatalog = true, true, true, true
	case registry.HarnessAgy:
		capabilities.RunningIdle, capabilities.WaitingPermission = true, true
	case registry.HarnessKilo:
		capabilities.SessionStart, capabilities.RunningIdle, capabilities.WaitingPermission, capabilities.NativeCatalog = true, true, true, true
	case registry.HarnessDroid:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle = true, true, true
	case registry.HarnessOpenClaw:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle = true, true, true
	case registry.HarnessHermes:
		capabilities.SessionStart, capabilities.SessionEnd, capabilities.RunningIdle, capabilities.WaitingPermission = true, true, true, true
	default:
		return Capabilities{}
	}
	return capabilities
}

type Adapter interface {
	Definition() Definition
}

type Installable interface {
	InstallPlan(binary string) InstallPlan
}

type Resumable interface {
	ResumeCommand(sessionID string, sessionPath string) []string
}

type PayloadAdapter interface {
	PayloadCompatible(rawPayload json.RawMessage) bool
	PayloadDefaults(payload map[string]any) PayloadDefaults
}

type baseAdapter struct {
	definition Definition
}

func (adapter baseAdapter) Definition() Definition {
	return cloneDefinition(adapter.definition)
}

func newBaseAdapter(definition Definition) baseAdapter {
	return baseAdapter{definition: cloneDefinition(definition)}
}

func newMetadataAdapter(id registry.Harness, env EnvKeys) baseAdapter {
	metadata, ok := harnessmeta.ByID(string(id))
	if !ok {
		panic("missing harness metadata for " + string(id))
	}

	return newBaseAdapter(Definition{
		ID:           id,
		Aliases:      metadata.Aliases,
		ProcessNames: metadata.ProcessNames,
		Env:          env,
		Capabilities: capabilitiesFor(id),
	})
}

func All() []Adapter {
	return append([]Adapter(nil), adapters...)
}

func Find(harness registry.Harness) (Adapter, bool) {
	for _, adapter := range adapters {
		if adapter.Definition().ID == harness {
			return adapter, true
		}
	}

	return nil, false
}

func Normalize(value string) (registry.Harness, error) {
	if id, ok := harnessmeta.Normalize(value); ok {
		return registry.Harness(id), nil
	}

	return "", fmt.Errorf("%w: %q", registry.ErrUnknownHarness, value)
}

func SupportedNames() []string {
	names := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		names = append(names, string(adapter.Definition().ID))
	}

	return names
}

func EnvNames(field EnvField) []string {
	names := genericEnvNames(field)
	for _, adapter := range adapters {
		names = appendUnique(names, envNamesForField(adapter.Definition().Env, field)...)
	}

	return names
}

func FromCommand(command string) (registry.Harness, bool) {
	if id, ok := harnessmeta.FromCommand(command); ok {
		return registry.Harness(id), true
	}

	return "", false
}

// ProcessNames returns executable names that identify a harness process.
func ProcessNames(harness registry.Harness) []string {
	return harnessmeta.ProcessNames(string(harness))
}

func DefaultsFromPayload(harness registry.Harness, rawPayload json.RawMessage) PayloadDefaults {
	if len(rawPayload) == 0 {
		return emptyPayloadDefaults()
	}

	adapter, ok := Find(harness)
	if !ok {
		return emptyPayloadDefaults()
	}
	payloadAdapter, ok := adapter.(PayloadAdapter)
	if !ok {
		return emptyPayloadDefaults()
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return emptyPayloadDefaults()
	}

	return payloadAdapter.PayloadDefaults(payload)
}

func PayloadCompatibleWithHarness(harness registry.Harness, rawPayload json.RawMessage) bool {
	if len(rawPayload) == 0 {
		return true
	}

	adapter, ok := Find(harness)
	if !ok {
		return true
	}
	payloadAdapter, ok := adapter.(PayloadAdapter)
	if !ok {
		return true
	}

	return payloadAdapter.PayloadCompatible(rawPayload)
}

func ResumeCommandFor(harness registry.Harness, sessionID string, sessionPath string) []string {
	adapter, ok := Find(harness)
	if !ok {
		return nil
	}
	resumable, ok := adapter.(Resumable)
	if !ok {
		return nil
	}

	return resumable.ResumeCommand(sessionID, sessionPath)
}

func WithResumeCommand(observation registry.Observation) registry.Observation {
	if observation.Catalog != nil && len(observation.Catalog.ResumeCommand) > 0 {
		return observation
	}
	command := ResumeCommandFor(
		observation.Harness,
		observation.Identity.SessionID,
		observation.Identity.SessionPath,
	)
	if len(command) == 0 {
		return observation
	}
	if observation.Catalog == nil {
		observation.Catalog = &registry.CatalogMetadata{ResumeCommand: nil, CWD: "", ProjectRoot: "", ProcessPID: 0, Current: false}
	}
	observation.Catalog.ResumeCommand = command
	return observation
}

func cloneDefinition(definition Definition) Definition {
	return Definition{
		ID:           definition.ID,
		Aliases:      cloneStrings(definition.Aliases),
		ProcessNames: cloneStrings(definition.ProcessNames),
		Env:          cloneEnvKeys(definition.Env),
		Capabilities: definition.Capabilities,
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
