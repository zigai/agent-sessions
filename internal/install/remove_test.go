package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnesspkg "github.com/zigai/aht/internal/harness"
	"github.com/zigai/aht/pkg/registry"
)

var errTestManifestWrite = errors.New("manifest write failed")

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
	t.Setenv("KIMI_SHARE_DIR", filepath.Join(home, ".kimi"))
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

func TestRemoveDeletesStaleManagedRenderedArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	path := filepath.Join(dir, "extensions", piExtensionName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := "aht managed integration\nAHT_INTEGRATION_ID=pi\nAHT_INTEGRATION_VERSION=5\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Remove(Options{Harness: registry.HarnessPi, Binary: testInstallBinary})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("stale integration removal did not report a change")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale managed artifact still exists: %v", err)
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
	if !strings.Contains(string(data), userCommand) || strings.Contains(string(data), "aht_integration=claude-hook") {
		t.Fatalf("user hook was not preserved cleanly: %s", data)
	}
}

func TestInspectReportsManagedCommandWithUnexpectedBinaryAsStale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if _, err := Run(Options{Harness: registry.HarnessClaude, Binary: testInstallBinary}); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(registry.HarnessClaude, "/different/aht")
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

func TestRemoveRefusesExtensionOwnedByAnotherHarness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	t.Setenv(registry.StateDirEnv, t.TempDir())

	installed, err := Run(Options{Harness: registry.HarnessOmp, Binary: testInstallBinary})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(Options{Harness: registry.HarnessPi, Binary: testInstallBinary}); !errors.Is(err, errForeignFile) {
		t.Fatalf("Pi removal error = %v, want errForeignFile", err)
	}
	data, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatalf("OMP extension was removed: %v", err)
	}
	if !strings.Contains(string(data), "AHT_INTEGRATION_ID=omp") {
		t.Fatalf("unexpected shared extension contents: %s", data)
	}
}

func TestPluginRemovalRollsBackDirectoryWhenManifestWriteFails(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pluginFile := filepath.Join(pluginDir, "index.ts")
	if err := os.WriteFile(pluginFile, []byte("managed plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "import_manifest.json")
	originalManifest := []byte("{\n  \"imports\": []\n}\n")
	if err := os.WriteFile(manifestPath, originalManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := harnesspkg.PluginDirectoryInstallPlan{
		Dir: pluginDir,
		ImportManifest: &harnesspkg.ImportManifestInstallPlan{
			Path: manifestPath,
		},
	}
	err := applyPluginRemovalWithWriter(
		plan,
		true,
		true,
		importManifest{Imports: nil},
		nil,
		func(string, importManifest) error { return errTestManifestWrite },
	)
	if !errors.Is(err, errTestManifestWrite) {
		t.Fatalf("plugin removal error = %v, want injected manifest failure", err)
	}
	if data, err := os.ReadFile(pluginFile); err != nil || string(data) != "managed plugin" {
		t.Fatalf("plugin directory was not restored: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(manifestPath); err != nil || string(data) != string(originalManifest) {
		t.Fatalf("manifest rollback = %q, err=%v", data, err)
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
