package observer

import (
	"testing"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestResolveHarnessUsesScopedAgentHint(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{Executable: "/usr/bin/fence", AgentHint: "claude", Args: []string{"fence", "--", "node"}}
	harness, ok := resolveHarness(process)
	if !ok || harness != registry.HarnessClaude {
		t.Fatalf("resolveHarness = %q, %v; want Claude from hint", harness, ok)
	}
}

func TestResolveHarnessScansKnownWrappers(t *testing.T) {
	t.Parallel()
	for _, wrapper := range []string{"env", "fence", "bwrap", "bubblewrap", "mise", "nix-shell", "nix", "direnv"} {
		t.Run(wrapper, func(t *testing.T) {
			t.Parallel()
			process := processinfo.Process{Executable: "/usr/bin/" + wrapper, Args: []string{wrapper, "--wrapper-option", "value", "--", "/usr/bin/codex"}}
			harness, ok := resolveHarness(process)
			if !ok || harness != registry.HarnessCodex {
				t.Fatalf("resolveHarness = %q, %v; want Codex behind %s", harness, ok, wrapper)
			}
		})
	}
}
