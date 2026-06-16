package cli

import "testing"

func TestRootCommandHasUse(t *testing.T) {
	t.Parallel()

	if rootCmd.Use != "agent-sessions" {
		t.Fatalf("expected root command use to be agent-sessions, got %q", rootCmd.Use)
	}
}
