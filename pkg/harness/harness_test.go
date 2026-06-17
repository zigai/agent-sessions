package harness

import (
	"slices"
	"testing"

	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestResumeCommandFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		harness     registry.Harness
		sessionID   string
		sessionPath string
		want        []string
	}{
		{
			name:        "codex",
			harness:     registry.HarnessCodex,
			sessionID:   "abc",
			sessionPath: "",
			want:        []string{"codex", "resume", "abc"},
		},
		{
			name:        "pi path",
			harness:     registry.HarnessPi,
			sessionID:   "abc",
			sessionPath: "/tmp/session.jsonl",
			want:        []string{"pi", "--session", "/tmp/session.jsonl"},
		},
		{
			name:        "opencode",
			harness:     registry.HarnessOpenCode,
			sessionID:   "abc",
			sessionPath: "",
			want:        []string{"opencode", "--session", "abc"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ResumeCommandFor(test.harness, test.sessionID, test.sessionPath)
			if !slices.Equal(got, test.want) {
				t.Fatalf("expected %#v, got %#v", test.want, got)
			}
		})
	}
}
