package harness

import (
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	ManagedMarker         = "agent-sessions managed integration"
	HookTimeoutSeconds    = 5
	HookEventSessionStart = "SessionStart"
	HookEventStop         = "Stop"
)

type InstallPlan struct {
	JSONCommandHooks *JSONCommandHookInstallPlan
	CursorJSONHooks  *CursorJSONHookInstallPlan
	ManagedTextBlock *ManagedTextBlockInstallPlan
	RenderedFile     *RenderedFileInstallPlan
	PluginDirectory  *PluginDirectoryInstallPlan
	Shim             *ShimInstallPlan
}

type JSONCommandHookInstallPlan struct {
	Path          string
	Source        string
	Label         string
	ConfigLabel   string
	StatusMessage string
	Hooks         []CommandHookInstallSpec
}

type CommandHookInstallSpec struct {
	Event   string
	Matcher string
	Command string
}

type CursorJSONHookInstallPlan struct {
	Path        string
	Source      string
	Label       string
	ConfigLabel string
	Hooks       []CursorCommandHookInstallSpec
}

type CursorCommandHookInstallSpec struct {
	Event   string
	Command string
}

type ManagedTextBlockInstallPlan struct {
	Path        string
	Label       string
	ConfigLabel string
	StartMarker string
	EndMarker   string
	Block       string
}

type RenderedFileInstallPlan struct {
	Path        string
	Label       string
	ConfigLabel string
	Content     string
	JSONContent any
}

type PluginDirectoryInstallPlan struct {
	Dir            string
	Label          string
	Files          []PluginFileInstallSpec
	SnippetOrder   []string
	MarkerFile     string
	ImportManifest *ImportManifestInstallPlan
}

type PluginFileInstallSpec struct {
	Name        string
	Content     string
	JSONContent any
}

type ImportManifestInstallPlan struct {
	Path       string
	Name       string
	Source     string
	Components []string
}

type ShimInstallPlan struct{}

func ReportHookCommand(binary string, harness registry.Harness, state registry.State, event string, source string) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"report",
		"--harness", ShellQuote(string(harness)),
		"--state", ShellQuote(string(state)),
		"--event", ShellQuote(event),
		"--source", ShellQuote(source),
		"--attribute", ShellQuote("agent_sessions_integration=" + source),
		"--raw-stdin",
		"--quiet",
	}, " ")
}

func RawStdinDefaultsReportHookCommand(
	binary string,
	harness registry.Harness,
	state registry.State,
	event string,
	source string,
) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"report",
		"--harness", ShellQuote(string(harness)),
		"--state", ShellQuote(string(state)),
		"--event", ShellQuote(event),
		"--source", ShellQuote(source),
		"--attribute", ShellQuote("agent_sessions_integration=" + source),
		"--raw-stdin-defaults-only",
		"--quiet",
	}, " ")
}

func SelfRefreshCommand(binary string, harness registry.Harness, stdinNull bool) string {
	parts := []string{
		ShellQuote(binary),
		"install-hooks", ShellQuote(string(harness)),
		"--binary", ShellQuote(binary),
	}
	if stdinNull {
		parts = append(parts, "</dev/null")
	}
	parts = append(parts, ">/dev/null", "2>&1", "&")

	return strings.Join(parts, " ")
}

func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}

	if isSafeShellWord(value) {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func renderScriptTemplate(template string, integrationID string, integrationVersion int, binary string, source string) string {
	return strings.NewReplacer(
		"{{MANAGED_MARKER}}", ManagedMarker,
		"{{INTEGRATION_ID}}", integrationID,
		"{{INTEGRATION_VERSION}}", strconv.Itoa(integrationVersion),
		"{{BINARY}}", strconv.Quote(binary),
		"{{SOURCE}}", strconv.Quote(source),
	).Replace(template)
}

func isSafeShellWord(value string) bool {
	for _, r := range value {
		if !isSafeShellRune(r) {
			return false
		}
	}

	return true
}

func isSafeShellRune(r rune) bool {
	switch {
	case r == '/', r == '.', r == '_', r == '-', r == '+', r == ':', r == '=':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	default:
		return false
	}
}
