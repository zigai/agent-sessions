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
	Actions []InstallAction
}

type InstallAction interface {
	installAction()
}

type JSONCommandHooksAction struct {
	Plan JSONCommandHookInstallPlan
}

func (JSONCommandHooksAction) installAction() {}

type CursorJSONHooksAction struct {
	Plan CursorJSONHookInstallPlan
}

func (CursorJSONHooksAction) installAction() {}

type ManagedTextBlockAction struct {
	Plan ManagedTextBlockInstallPlan
}

func (ManagedTextBlockAction) installAction() {}

type RenderedFileAction struct {
	Plan RenderedFileInstallPlan
}

func (RenderedFileAction) installAction() {}

type PluginDirectoryAction struct {
	Plan PluginDirectoryInstallPlan
}

func (PluginDirectoryAction) installAction() {}

type ShimAction struct{}

func (ShimAction) installAction() {}

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

func ReportHookCommand(binary string, harness registry.Harness, state registry.State, event string, source string) string {
	return reportHookCommand(binary, harness, state, event, source, "--raw-stdin")
}

func RawStdinDefaultsReportHookCommand(
	binary string,
	harness registry.Harness,
	state registry.State,
	event string,
	source string,
) string {
	return reportHookCommand(binary, harness, state, event, source, "--raw-stdin-defaults-only")
}

func reportHookCommand(
	binary string,
	harness registry.Harness,
	state registry.State,
	event string,
	source string,
	stdinFlag string,
) string {
	return strings.Join([]string{
		ShellQuote(binary),
		"report",
		"--harness", ShellQuote(string(harness)),
		"--state", ShellQuote(string(state)),
		"--event", ShellQuote(event),
		"--source", ShellQuote(source),
		"--attribute", ShellQuote("agent_sessions_integration=" + source),
		"--queue",
		stdinFlag,
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
