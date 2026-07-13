package install

import "testing"

func TestClassifyArtifactContentAcceptsSourceMetadata(t *testing.T) {
	t.Parallel()

	current := `{"command":"agent-sessions report codex --attribute agent_sessions_integration_version=3 --attribute agent_sessions_integration=codex-hook"}`
	if status := classifyArtifactContent(current); status != ArtifactCurrent {
		t.Fatalf("current source metadata classified as %q", status)
	}

	stale := `{"command":"agent-sessions report codex --attribute agent_sessions_integration_version=2 --attribute agent_sessions_integration=codex-hook"}`
	if status := classifyArtifactContent(stale); status != ArtifactStale {
		t.Fatalf("stale source metadata classified as %q", status)
	}

	foreign := `{"hooks":{"Stop":[{"command":"custom-tool"}]}}`
	if status := classifyArtifactContent(foreign); status != ArtifactForeign {
		t.Fatalf("foreign content classified as %q", status)
	}
}
