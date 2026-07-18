package harness

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	openClawCommand           = "openclaw"
	openClawPluginName        = "agent-sessions-state"
	openClawMarkerFileName    = ".agent-sessions-managed"
	openClawIntegrationSource = "openclaw-plugin"
)

//go:embed assets/openclaw/index.js.tmpl
var openClawPluginTemplate string

type openClawHarness struct {
	baseAdapter
}

func openClawAdapter() Adapter {
	return openClawHarness{baseAdapter: newMetadataAdapter(registry.HarnessOpenClaw, EnvKeys{
		SessionID: nil, SessionPath: nil, ProjectRoot: nil, PID: nil, Event: nil,
	})}
}

func (openClawHarness) InstallPlan(binary string) InstallPlan {
	version := strconv.Itoa(IntegrationVersionFor(registry.HarnessOpenClaw))
	dir := filepath.Join(registry.DefaultStateDir(), "integrations", "openclaw", openClawPluginName)

	return InstallPlan{Actions: []InstallAction{PluginDirectoryAction{Plan: PluginDirectoryInstallPlan{
		Dir:   dir,
		Label: "OpenClaw plugin",
		Files: []PluginFileInstallSpec{
			{Name: "package.json", Content: "", JSONContent: map[string]any{
				"name": openClawPluginName, "version": "0.0." + version, "private": true, "type": "module",
				"openclaw": map[string]any{"extensions": []string{"./index.js"}},
			}},
			{Name: "openclaw.plugin.json", Content: "", JSONContent: map[string]any{
				"id": openClawPluginName, "name": "Agent Sessions State", "version": "0.0." + version,
				"description":  "Reports local OpenClaw session lifecycle and activity to agent-sessions.",
				"activation":   map[string]any{"onStartup": true},
				"configSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}},
			}},
			{Name: "index.js", Content: renderOpenClawPlugin(binary, version), JSONContent: nil},
			{Name: openClawMarkerFileName, Content: openClawMarkerContent(version), JSONContent: nil},
		},
		SnippetOrder:   []string{"package.json", "openclaw.plugin.json", "index.js", openClawMarkerFileName},
		MarkerFile:     openClawMarkerFileName,
		ImportManifest: nil,
		OpenClaw: &OpenClawPluginRegistrationPlan{
			Command: openClawCommand, PluginID: openClawPluginName, Version: "0.0." + version,
			AllowConversationAccess: true,
		},
		Hermes: nil,
	}}}}
}

func renderOpenClawPlugin(binary string, version string) string {
	replacer := strings.NewReplacer(
		"{{BINARY}}", strconv.Quote(binary),
		"{{INTEGRATION_VERSION}}", strconv.Quote(version),
	)

	return replacer.Replace(openClawPluginTemplate)
}

func openClawMarkerContent(version string) string {
	return fmt.Sprintf("%s\nAGENT_SESSIONS_INTEGRATION_ID=openclaw\nAGENT_SESSIONS_INTEGRATION_VERSION=%s\nAGENT_SESSIONS_SOURCE=%s\n", ManagedMarker, version, openClawIntegrationSource)
}
