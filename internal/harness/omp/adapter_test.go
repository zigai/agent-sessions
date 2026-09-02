package omp

import (
	"strings"
	"testing"
)

func TestPluginTemplateOwnsSpawnErrors(t *testing.T) {
	t.Parallel()

	if !strings.Contains(ompExtensionTemplate, `child.on("error", () => {});`) &&
		!strings.Contains(ompExtensionTemplate, `child.once("error", finish);`) {
		t.Fatal("expected asynchronous child error handling")
	}
}

func TestAgentDirUsesExplicitEmptyProfile(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", "/tmp/omp")
	t.Setenv("PI_PROFILE", "legacy")
	t.Setenv("OMP_PROFILE", "")
	if got := ompAgentDir(); got != "/tmp/omp/agent" {
		t.Fatalf("expected explicit empty OMP_PROFILE to select default, got %q", got)
	}
}
