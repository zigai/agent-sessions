package harnessmeta

import (
	"path/filepath"
	"strings"
)

// Definition is the canonical static metadata for a supported harness.
type Definition struct {
	ID           string
	Aliases      []string
	ProcessNames []string
}

const (
	IDClaude   = "claude"
	IDCodex    = "codex"
	IDCursor   = "cursor"
	IDCopilot  = "copilot"
	IDCline    = "cline"
	IDKimiCode = "kimi-code"
	IDGrok     = "grok"
	IDGoose    = "goose"
	IDPi       = "pi"
	IDOpenCode = "opencode"
	IDAgy      = "agy"
	IDKilo     = "kilo"
	IDDroid    = "droid"
)

var definitions = []Definition{
	{
		ID:           IDClaude,
		Aliases:      []string{"claude-code", "claude_code"},
		ProcessNames: []string{"claude", "claude-code"},
	},
	{
		ID:           IDCodex,
		Aliases:      nil,
		ProcessNames: []string{"codex"},
	},
	{
		ID:           IDCursor,
		Aliases:      []string{"cursor-agent", "cursor_agent", "cursor-cli", "cursor_cli"},
		ProcessNames: []string{"cursor", "cursor-agent", "cursor-cli"},
	},
	{
		ID:           IDCopilot,
		Aliases:      []string{"github-copilot", "github_copilot", "copilot-cli", "copilot_cli", "github-copilot-cli", "github_copilot_cli"},
		ProcessNames: []string{"copilot"},
	},
	{
		ID:           IDCline,
		Aliases:      nil,
		ProcessNames: []string{"cline"},
	},
	{
		ID:           IDKimiCode,
		Aliases:      []string{"kimi", "kimi_code", "kimicode"},
		ProcessNames: []string{"kimi", "kimi-code", "kimi_code", "kimicode"},
	},
	{
		ID:           IDGrok,
		Aliases:      []string{"grok-build", "grok_build"},
		ProcessNames: []string{"grok", "grok-build"},
	},
	{
		ID:           IDGoose,
		Aliases:      nil,
		ProcessNames: []string{"goose"},
	},
	{
		ID:           IDPi,
		Aliases:      nil,
		ProcessNames: []string{"pi"},
	},
	{
		ID:           IDOpenCode,
		Aliases:      []string{"open-code", "open_code"},
		ProcessNames: []string{"opencode", "open-code"},
	},
	{
		ID: IDAgy,
		Aliases: []string{
			"antigravity",
			"antigravity-cli",
			"antigravity_cli",
			"google-antigravity",
			"google_antigravity",
		},
		ProcessNames: []string{"agy", "antigravity", "antigravity-cli"},
	},
	{
		ID:           IDKilo,
		Aliases:      []string{"kilocode", "kilo-code", "kilo_code"},
		ProcessNames: []string{"kilo", "kilocode", "kilo-code", "kilo_code"},
	},
	{
		ID: IDDroid,
		Aliases: []string{
			"factory",
			"factory-droid",
			"factory_droid",
			"factory-cli",
			"factory_cli",
		},
		ProcessNames: []string{"droid"},
	},
}

// ByID returns the metadata for a harness id.
func ByID(id string) (Definition, bool) {
	normalized := normalizeToken(id)
	for _, definition := range definitions {
		if normalizeToken(definition.ID) == normalized {
			return cloneDefinition(definition), true
		}
	}

	return Definition{ID: "", Aliases: nil, ProcessNames: nil}, false
}

// Normalize resolves a harness id or alias to its canonical id.
func Normalize(value string) (string, bool) {
	normalized := normalizeToken(value)
	for _, definition := range definitions {
		if normalized == normalizeToken(definition.ID) {
			return definition.ID, true
		}
		for _, alias := range definition.Aliases {
			if normalized == normalizeToken(alias) {
				return definition.ID, true
			}
		}
	}

	return "", false
}

// FromCommand resolves an executable name or path to a harness id.
func FromCommand(command string) (string, bool) {
	normalized := normalizeToken(filepath.Base(command))
	for _, definition := range definitions {
		for _, processName := range definition.ProcessNames {
			if normalized == normalizeToken(processName) {
				return definition.ID, true
			}
		}
	}

	return "", false
}

// ProcessNames returns the process names associated with a harness id.
func ProcessNames(id string) []string {
	definition, ok := ByID(id)
	if !ok {
		return nil
	}

	return definition.ProcessNames
}

func cloneDefinition(definition Definition) Definition {
	return Definition{
		ID:           definition.ID,
		Aliases:      append([]string(nil), definition.Aliases...),
		ProcessNames: append([]string(nil), definition.ProcessNames...),
	}
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
