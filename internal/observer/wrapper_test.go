package observer

import (
	"testing"

	"github.com/zigai/aht/internal/processinfo"
	"github.com/zigai/aht/pkg/registry"
)

func TestResolveHarnessUsesScopedAgentHint(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{Executable: "/usr/bin/fence", AgentHint: "claude", Args: []string{"fence", "--", "node"}}
	harness, ok := resolveHarness(process)
	if !ok || harness != registry.HarnessClaude {
		t.Fatalf("resolveHarness = %q, %v; want Claude from hint", harness, ok)
	}
}

func TestResolveHarnessIgnoresOmpInternalWorkers(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{
		Executable: "/home/test/.local/bin/omp",
		Args:       []string{"/home/test/.local/bin/omp", "__omp_worker_js_eval_process"},
	}
	harness, ok := resolveHarness(process)
	if ok || harness != "" {
		t.Fatalf("resolveHarness = %q, %v; want internal OMP worker ignored", harness, ok)
	}
}

func TestResolveHarnessKeepsOmpHeadlessSessions(t *testing.T) {
	t.Parallel()
	process := processinfo.Process{
		Executable: "/home/test/.local/bin/omp",
		Args:       []string{"/home/test/.local/bin/omp", "--print", "check this repository"},
	}
	harness, ok := resolveHarness(process)
	if !ok || harness != registry.HarnessOmp {
		t.Fatalf("resolveHarness = %q, %v; want headless OMP session", harness, ok)
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
