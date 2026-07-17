package cli

import (
	"errors"
	"testing"
)

func TestParseFastReportOptionsSupportsProcessEvidence(t *testing.T) {
	t.Parallel()

	options, ok, err := parseFastReportOptions([]string{
		"droid", "--presence", "live", "--evidence", "process", "--pid", "42", "--event", "process.start", "--queue", "--quiet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || options.harness != "droid" || options.evidence != "process" || options.pid != 42 || !options.queue || !options.quiet {
		t.Fatalf("fast process options = %#v, ok=%v", options, ok)
	}
}

func TestParseFastReportOptionsRejectsConflictingHarnesses(t *testing.T) {
	t.Setenv("AGENT_SESSIONS_HARNESS", "codex")

	_, ok, err := parseFastReportOptions([]string{"claude", "--session-id", "session"})
	if !ok || !errors.Is(err, errUnexpectedReportArg) {
		t.Fatalf("conflicting harness error = %v, ok=%v", err, ok)
	}
}
