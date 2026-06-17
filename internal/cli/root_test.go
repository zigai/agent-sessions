package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestRootCommandHasUse(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	if cmd.Use != "agent-sessions" {
		t.Fatalf("expected root command use to be agent-sessions, got %q", cmd.Use)
	}
}

func TestReportHelpListsSupportedHarnesses(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{"report", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	got := output.String()
	for _, harness := range []string{"codex", "grok", "pi", "opencode"} {
		if !strings.Contains(got, harness) {
			t.Fatalf("expected report help to include %s, got %q", harness, got)
		}
	}
}

func TestReportAndList(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	reportCmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	reportCmd.SetArgs([]string{
		"--store", storePath,
		"report",
		"--harness", "codex",
		"--state", "running",
		"--session-id", "abc",
		"--no-tmux",
	})
	if err := reportCmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{"--store", storePath, "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	output := listOut.String()
	if !strings.Contains(output, "codex") {
		t.Fatalf("expected list output to include codex, got %q", output)
	}
	if !strings.Contains(output, "running") {
		t.Fatalf("expected list output to include running, got %q", output)
	}
	if !strings.Contains(output, "ago") && !strings.Contains(output, "just now") {
		t.Fatalf("expected list output to show relative updated time, got %q", output)
	}
	if rfc3339Pattern().MatchString(output) {
		t.Fatalf("expected default list output not to include RFC3339 timestamp, got %q", output)
	}
}

func TestListAbsoluteTime(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	reportCmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	reportCmd.SetArgs([]string{
		"--store", storePath,
		"report",
		"--harness", "codex",
		"--state", "running",
		"--session-id", "abc",
		"--no-tmux",
	})
	if err := reportCmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{"--store", storePath, "list", "--absolute-time"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	output := listOut.String()
	if !rfc3339Pattern().MatchString(output) {
		t.Fatalf("expected absolute list output to include RFC3339 timestamp, got %q", output)
	}
}

func TestListSortUpdatedDesc(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	ctx := context.Background()
	oldSession, err := store.Report(ctx, registry.Report{
		Harness:    registry.HarnessCodex,
		State:      registry.StateIdle,
		SessionID:  "old",
		ObservedAt: time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("reporting old session: %v", err)
	}
	newSession, err := store.Report(ctx, registry.Report{
		Harness:    registry.HarnessCodex,
		State:      registry.StateRunning,
		SessionID:  "new",
		ObservedAt: time.Date(2026, 6, 17, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("reporting new session: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{"--store", storePath, "list", "--sort", "updated", "--desc", "--absolute-time"})
	executeErr := listCmd.Execute()
	if executeErr != nil {
		t.Fatalf("list command failed: %v", executeErr)
	}

	output := listOut.String()
	newIndex := strings.Index(output, newSession.ID)
	oldIndex := strings.Index(output, oldSession.ID)
	if newIndex < 0 || oldIndex < 0 {
		t.Fatalf("expected both sessions in output, got %q", output)
	}
	if newIndex > oldIndex {
		t.Fatalf("expected newest session first, got %q", output)
	}
}

func TestListSortRejectsUnknownField(t *testing.T) {
	t.Parallel()

	var listOut bytes.Buffer
	var listErr bytes.Buffer
	listCmd := NewRootCommand(&listOut, &listErr)
	listCmd.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "state.json"), "list", "--sort", "nope"})
	err := listCmd.Execute()
	if err == nil {
		t.Fatal("expected invalid sort error")
	}
	if !strings.Contains(err.Error(), "invalid list sort") {
		t.Fatalf("expected invalid sort error, got %v", err)
	}
}

func TestFormatUpdatedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		updatedAt time.Time
		absolute  bool
		want      string
	}{
		{
			name:      "zero",
			updatedAt: time.Time{},
			absolute:  false,
			want:      "-",
		},
		{
			name:      "just now",
			updatedAt: now.Add(-500 * time.Millisecond),
			absolute:  false,
			want:      "just now",
		},
		{
			name:      "minutes",
			updatedAt: now.Add(-3 * time.Minute),
			absolute:  false,
			want:      "3m ago",
		},
		{
			name:      "absolute",
			updatedAt: now,
			absolute:  true,
			want:      "2026-06-17T12:00:00Z",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := formatUpdatedAt(test.updatedAt, now, test.absolute)
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func rfc3339Pattern() *regexp.Regexp {
	return regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)
}

func TestReportCodexHookPayloadQuiet(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	cmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	cmd.SetArgs([]string{
		"--store", storePath,
		"report",
		"--harness", "codex",
		"--state", "running",
		"--source", "codex-hook",
		"--raw-stdin",
		"--quiet",
		"--no-tmux",
	})
	cmd.SetIn(strings.NewReader(`{"session_id":"codex-session","transcript_path":"/tmp/codex.jsonl","cwd":"/tmp","hook_event_name":"UserPromptSubmit","model":"gpt-5-codex"}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}
	if reportOut.String() != "" {
		t.Fatalf("expected quiet report to suppress output, got %q", reportOut.String())
	}

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{
		Harness:     registry.HarnessCodex,
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("listing sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "codex-session" {
		t.Fatalf("expected codex session id, got %q", sessions[0].SessionID)
	}
	if sessions[0].SessionPath != "/tmp/codex.jsonl" {
		t.Fatalf("expected codex transcript path, got %q", sessions[0].SessionPath)
	}
	if sessions[0].Attributes["codex_hook_event"] != "UserPromptSubmit" {
		t.Fatalf("expected codex hook event attribute, got %#v", sessions[0].Attributes)
	}
	if sessions[0].LastEvent != "UserPromptSubmit" {
		t.Fatalf("expected codex last event, got %q", sessions[0].LastEvent)
	}
	if sessions[0].LastEventAt.IsZero() || sessions[0].StateChangedAt.IsZero() {
		t.Fatalf("expected event and state timestamps, got event_at=%s state_changed_at=%s", sessions[0].LastEventAt, sessions[0].StateChangedAt)
	}
}

func TestReportGrokHookPayloadQuiet(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	cmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	cmd.SetArgs([]string{
		"--store", storePath,
		"report",
		"--harness", "grok",
		"--state", "running",
		"--source", "grok-hook",
		"--raw-stdin",
		"--quiet",
		"--no-tmux",
	})
	cmd.SetIn(strings.NewReader(`{"sessionId":"grok-session","cwd":"/tmp","workspaceRoot":"/tmp","hookEventName":"UserPromptSubmit","toolName":"run_terminal_command"}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}
	if reportOut.String() != "" {
		t.Fatalf("expected quiet report to suppress output, got %q", reportOut.String())
	}

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{
		Harness:     registry.HarnessGrok,
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("listing sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "grok-session" {
		t.Fatalf("expected grok session id, got %q", sessions[0].SessionID)
	}
	if sessions[0].CWD != "/tmp" {
		t.Fatalf("expected grok cwd, got %q", sessions[0].CWD)
	}
	if sessions[0].Attributes["grok_hook_event"] != "UserPromptSubmit" {
		t.Fatalf("expected grok hook event attribute, got %#v", sessions[0].Attributes)
	}
	if sessions[0].Attributes["grok_tool_name"] != "run_terminal_command" {
		t.Fatalf("expected grok tool name attribute, got %#v", sessions[0].Attributes)
	}
	if sessions[0].LastEvent != "UserPromptSubmit" {
		t.Fatalf("expected grok last event, got %q", sessions[0].LastEvent)
	}
	if sessions[0].LastEventAt.IsZero() || sessions[0].StateChangedAt.IsZero() {
		t.Fatalf("expected event and state timestamps, got event_at=%s state_changed_at=%s", sessions[0].LastEventAt, sessions[0].StateChangedAt)
	}
}

func TestDefaultInstallBinaryIsAbsolute(t *testing.T) {
	t.Parallel()

	got := defaultInstallBinary()
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute default install binary, got %q", got)
	}
}

func TestInstallHooksAll(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("AGENT_SESSIONS_STATE_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		"install-hooks",
		"all",
		"--binary", "agent-sessions",
		"--target-binary", "/usr/bin/opencode",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install-hooks all failed: %v", err)
	}

	got := output.String()
	for _, harness := range []string{"codex", "grok", "pi", "opencode"} {
		if !strings.Contains(got, harness) {
			t.Fatalf("expected output to include %s, got %q", harness, got)
		}
	}
}

func TestInstallHooksUsesAbsoluteDefaultBinary(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		"install-hooks",
		"pi",
		"--dry-run",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install-hooks pi dry-run failed: %v", err)
	}

	want := defaultInstallBinary()
	if !strings.Contains(output.String(), want) {
		t.Fatalf("expected dry-run output to include default binary %q, got %q", want, output.String())
	}
}
