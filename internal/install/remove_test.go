package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

//nolint:gocognit,cyclop // one uniform round trip verifies every managed artifact shape
func TestInstallRemoveRoundTripForEveryHarness(t *testing.T) {
	installFakeOpenClawCLI(t)
	installFakeHermesCLI(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("CLINE_DIR", filepath.Join(home, ".cline"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code"))
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	t.Setenv("AGY_CONFIG_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))

	for _, harnessID := range AllHarnesses() {
		result, err := Run(Options{Harness: harnessID, Binary: testInstallBinary})
		if err != nil {
			t.Fatalf("install %s: %v", harnessID, err)
		}
		if !result.Changed {
			t.Fatalf("install %s did not change artifact", harnessID)
		}
		status, err := Inspect(harnessID, testInstallBinary)
		if err != nil {
			t.Fatalf("inspect installed %s: %v", harnessID, err)
		}
		if status.Status != ArtifactCurrent {
			t.Fatalf("installed %s status = %q: %#v", harnessID, status.Status, status)
		}
		dryRun, err := Remove(Options{Harness: harnessID, Binary: testInstallBinary, DryRun: true})
		if err != nil || !dryRun.Changed {
			t.Fatalf("dry-run removal of %s = %+v, %v", harnessID, dryRun, err)
		}
		status, err = Inspect(harnessID, testInstallBinary)
		if err != nil || status.Status != ArtifactCurrent {
			t.Fatalf("dry-run removal changed %s: %+v, %v", harnessID, status, err)
		}

		removed, err := Remove(Options{Harness: harnessID, Binary: testInstallBinary})
		if err != nil {
			t.Fatalf("remove %s: %v", harnessID, err)
		}
		if !removed.Changed {
			t.Fatalf("remove %s did not change artifact", harnessID)
		}
		status, err = Inspect(harnessID, testInstallBinary)
		if err != nil {
			t.Fatalf("inspect removed %s: %v", harnessID, err)
		}
		if status.Status != ArtifactMissing {
			t.Fatalf("removed %s status = %q: %#v", harnessID, status.Status, status)
		}
		removed, err = Remove(Options{Harness: harnessID, Binary: testInstallBinary})
		if err != nil || removed.Changed {
			t.Fatalf("second removal of %s was not idempotent: %+v, %v", harnessID, removed, err)
		}
	}
}

func TestRemovePreservesUserHooksInSharedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "settings.json")
	userCommand := "custom-tool"
	initial := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + userCommand + `"}]}]}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Harness: registry.HarnessClaude, Binary: testInstallBinary}); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(Options{Harness: registry.HarnessClaude, Binary: testInstallBinary}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), userCommand) || strings.Contains(string(data), "agent_sessions_integration=claude-hook") {
		t.Fatalf("user hook was not preserved cleanly: %s", data)
	}
}

func TestInspectReportsManagedCommandWithUnexpectedBinaryAsStale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if _, err := Run(Options{Harness: registry.HarnessClaude, Binary: testInstallBinary}); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(registry.HarnessClaude, "/different/agent-sessions")
	if err != nil || status.Status != ArtifactStale {
		t.Fatalf("unexpected binary status = %+v, %v", status, err)
	}
}

func TestRemoveRefusesForeignOwnedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", dir)
	path := filepath.Join(dir, "hooks", copilotHookFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(registry.HarnessCopilot, testInstallBinary)
	if err != nil || status.Status != ArtifactForeign {
		t.Fatalf("foreign status = %+v, %v", status, err)
	}
	if _, err := Remove(Options{Harness: registry.HarnessCopilot, Binary: testInstallBinary}); err == nil {
		t.Fatal("expected foreign integration removal to fail")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("foreign file was removed: %v", err)
	}
}

func TestRemoveAlsoRemovesManagedShimFallback(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	state := t.TempDir()
	t.Setenv(registry.StateDirEnv, state)
	target := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}
	installed, err := Run(Options{Harness: registry.HarnessCodex, Binary: testInstallBinary, TargetBinary: target, UseShim: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed.Path); err != nil {
		t.Fatalf("managed shim missing after install: %v", err)
	}
	status, err := Inspect(registry.HarnessCodex, testInstallBinary)
	if err != nil || status.Status != ArtifactCurrent {
		t.Fatalf("shim status = %+v, %v", status, err)
	}
	removed, err := Remove(Options{Harness: registry.HarnessCodex, Binary: testInstallBinary})
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Changed || removed.Path != installed.Path {
		t.Fatalf("shim removal did not report a change: %+v", removed)
	}
	if _, err := os.Stat(installed.Path); !os.IsNotExist(err) {
		t.Fatalf("managed shim remains after removal: %v", err)
	}
}

func TestInspectDetectsAndInstallRepairsMissingPluginImport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGY_CONFIG_HOME", dir)
	if _, err := Run(Options{Harness: registry.HarnessAgy, Binary: testInstallBinary}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, agyImportManifestName)
	if err := os.WriteFile(manifestPath, []byte("{\n  \"imports\": []\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(registry.HarnessAgy, testInstallBinary)
	if err != nil || status.Status != ArtifactMissing {
		t.Fatalf("missing import status = %+v, %v", status, err)
	}
	repaired, err := Run(Options{Harness: registry.HarnessAgy, Binary: testInstallBinary})
	if err != nil || !repaired.Changed {
		t.Fatalf("repair install = %+v, %v", repaired, err)
	}
	status, err = Inspect(registry.HarnessAgy, testInstallBinary)
	if err != nil || status.Status != ArtifactCurrent {
		t.Fatalf("repaired import status = %+v, %v", status, err)
	}
}
