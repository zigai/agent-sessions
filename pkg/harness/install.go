package harness

import (
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	ManagedMarker               = "agent-sessions managed integration"
	HookTimeoutSeconds          = 5
	HookTypeCommand             = "command"
	HookEventSessionStart       = "SessionStart"
	HookEventUserPromptSubmit   = "UserPromptSubmit"
	HookEventPostToolUse        = "PostToolUse"
	HookEventPostToolUseFailure = "PostToolUseFailure"
	HookEventPreToolUse         = "PreToolUse"
	HookEventStop               = "Stop"
	resumeFlag                  = "--resume"
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

type RenderedFilesAction struct {
	Plan RenderedFilesInstallPlan
}

func (RenderedFilesAction) installAction() {}

type PluginDirectoryAction struct {
	Plan PluginDirectoryInstallPlan
}

func (PluginDirectoryAction) installAction() {}

type ShimAction struct{}

func (ShimAction) installAction() {}

type JSONCommandHookInstallPlan struct {
	Path              string
	Source            string
	Label             string
	ConfigLabel       string
	StatusMessage     string
	OmitStatusMessage bool
	Hooks             []CommandHookInstallSpec
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

type RenderedFilesInstallPlan struct {
	Dir          string
	Label        string
	ConfigLabel  string
	Files        []RenderedFileInstallSpec
	SnippetOrder []string
}

type RenderedFileInstallSpec struct {
	Name        string
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
	OpenClaw       *OpenClawPluginRegistrationPlan
	Hermes         *HermesPluginRegistrationPlan
}

// OpenClawPluginRegistrationPlan describes native OpenClaw CLI registration
// for a managed plugin source directory.
type OpenClawPluginRegistrationPlan struct {
	Command                 string
	PluginID                string
	Version                 string
	AllowConversationAccess bool
}

// HermesPluginRegistrationPlan describes native Hermes plugin activation for
// a managed plugin installed in the documented user plugin directory.
type HermesPluginRegistrationPlan struct {
	Command  string
	PluginID string
	Version  string
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

func ReportHookCommand(binary string, harness registry.Harness, transition any, event string, source string) string {
	return reportHookCommand(binary, harness, transition, event, source, "--raw-stdin")
}

func RawStdinDefaultsReportHookCommand(
	binary string,
	harness registry.Harness,
	transition any,
	event string,
	source string,
) string {
	return reportHookCommand(binary, harness, transition, event, source, "--raw-stdin-defaults-only")
}

func reportHookCommand(
	binary string,
	harness registry.Harness,
	transition any,
	event string,
	source string,
	stdinFlag string,
) string {
	parts := []string{
		ShellQuote(binary),
		"report",
		ShellQuote(string(harness)),
	}
	switch value := transition.(type) {
	case registry.Presence:
		if value != "" {
			parts = append(parts, "--presence", ShellQuote(string(value)))
		}
	case registry.Activity:
		if value != "" {
			parts = append(parts, "--activity", ShellQuote(string(value)))
		}
	case string:
		if value != "" {
			parts = append(parts, "--activity", ShellQuote(value))
		}
	}
	if event != "" {
		parts = append(parts, "--event", ShellQuote(event))
	}
	parts = append(
		parts,
		"--attribute", ShellQuote("agent_sessions_integration_version="+strconv.Itoa(IntegrationVersion)),
		"--attribute", ShellQuote("agent_sessions_integration="+source),
		"--queue",
		stdinFlag,
		"--quiet",
	)
	return strings.Join(parts, " ")
}

func stringTransition(value any) string {
	switch transition := value.(type) {
	case registry.Activity:
		return string(transition)
	case registry.Presence:
		return string(transition)
	case string:
		return transition
	default:
		return ""
	}
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

func renderScriptTemplate(template string, integrationID string, binary string, source string) string {
	return strings.NewReplacer(
		"{{MANAGED_MARKER}}", ManagedMarker,
		"{{INTEGRATION_ID}}", integrationID,
		"{{INTEGRATION_VERSION}}", strconv.Itoa(IntegrationVersion),
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
