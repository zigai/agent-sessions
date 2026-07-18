package harness

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	hermesCommand           = "hermes"
	hermesPluginName        = "agent-sessions-state"
	hermesMarkerFileName    = ".agent-sessions-managed"
	hermesIntegrationSource = "hermes-plugin"
)

//go:embed assets/hermes/__init__.py.tmpl
var hermesPluginTemplate string

type hermesHarness struct {
	baseAdapter
}

func hermesAdapter() Adapter {
	return hermesHarness{baseAdapter: newMetadataAdapter(registry.HarnessHermes, EnvKeys{
		SessionID: nil, SessionPath: nil, ProjectRoot: nil, PID: nil, Event: nil,
	})}
}

func (hermesHarness) InstallPlan(binary string) InstallPlan {
	version := strconv.Itoa(IntegrationVersionFor(registry.HarnessHermes))
	dir := filepath.Join(hermesHome(), "plugins", hermesPluginName)

	return InstallPlan{Actions: []InstallAction{PluginDirectoryAction{Plan: PluginDirectoryInstallPlan{
		Dir:   dir,
		Label: "Hermes plugin",
		Files: []PluginFileInstallSpec{
			{Name: "plugin.yaml", Content: hermesPluginManifest(version), JSONContent: nil},
			{Name: "__init__.py", Content: renderHermesPlugin(binary, version), JSONContent: nil},
			{Name: hermesMarkerFileName, Content: hermesMarkerContent(version), JSONContent: nil},
		},
		SnippetOrder:   []string{"plugin.yaml", "__init__.py", hermesMarkerFileName},
		MarkerFile:     hermesMarkerFileName,
		ImportManifest: nil,
		OpenClaw:       nil,
		Hermes: &HermesPluginRegistrationPlan{
			Command: hermesCommand, PluginID: hermesPluginName, Version: "0.0." + version,
		},
	}}}}
}

func (hermesHarness) ResumeCommand(sessionID string, _ string) []string {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}

	return []string{hermesCommand, resumeFlag, sessionID}
}

func hermesHome() string {
	if configured := strings.TrimSpace(os.Getenv("HERMES_HOME")); configured != "" {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".hermes")
	}

	return ".hermes"
}

func hermesPluginManifest(version string) string {
	return fmt.Sprintf(`name: agent-sessions-state
version: "0.0.%s"
description: Reports local Hermes session lifecycle and activity to agent-sessions.
provides_hooks:
  - on_session_start
  - pre_llm_call
  - on_session_end
  - on_session_finalize
  - on_session_reset
  - pre_approval_request
  - post_approval_response
`, version)
}

func renderHermesPlugin(binary string, version string) string {
	replacer := strings.NewReplacer(
		"{{BINARY}}", strconv.Quote(binary),
		"{{INTEGRATION_VERSION}}", strconv.Quote(version),
	)

	return replacer.Replace(hermesPluginTemplate)
}

func hermesMarkerContent(version string) string {
	return fmt.Sprintf("%s\nAGENT_SESSIONS_INTEGRATION_ID=hermes\nAGENT_SESSIONS_INTEGRATION_VERSION=%s\nAGENT_SESSIONS_SOURCE=%s\n", ManagedMarker, version, hermesIntegrationSource)
}
