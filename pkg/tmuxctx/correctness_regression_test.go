//go:build integration

package tmuxctx

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTmuxFormatQuotesBareVariableNames(t *testing.T) {
	t.Parallel()

	got := currentFormat()
	want := "tmuxctx:#{q:session_id} tmuxctx:#{q:session_name} tmuxctx:#{q:window_id} " +
		"tmuxctx:#{q:window_index} tmuxctx:#{q:window_name} tmuxctx:#{q:pane_id} " +
		"tmuxctx:#{q:pane_index} tmuxctx:#{q:pane_current_path} tmuxctx:#{q:pane_pid} " +
		"tmuxctx:#{q:pane_tty} tmuxctx:#{q:client_tty}"
	if got != want {
		t.Fatalf("tmux format = %q, want %q", got, want)
	}
}

func TestTmuxFormatWithRealTmuxEscapedFields(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	socket := filepath.Join(t.TempDir(), "tmux.sock")
	weirdValue := "value with spaces 'quote $dollar back\\slash\tand-tab"
	ctx := context.Background()
	defer func() {
		_ = exec.CommandContext(ctx, "tmux", "-S", socket, "kill-server").Run()
	}()

	if output, err := exec.CommandContext(
		ctx,
		"tmux",
		"-S",
		socket,
		"new-session",
		"-d",
	).CombinedOutput(); err != nil {
		t.Fatalf("starting tmux session: %v: %s", err, output)
	}
	if output, err := exec.CommandContext(ctx, "tmux", "-S", socket, "set-option", "-gq", "@agent_sessions_weird", weirdValue).CombinedOutput(); err != nil {
		t.Fatalf("setting tmux option: %v: %s", err, output)
	}

	output, err := exec.CommandContext(
		ctx,
		"tmux",
		"-S",
		socket,
		"display-message",
		"-p",
		"-F",
		tmuxFormat([]string{"@agent_sessions_weird"}),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("displaying tmux option: %v: %s", err, output)
	}
	fields, err := parseTmuxFields(string(output), 1)
	if err != nil {
		t.Fatalf("parseTmuxFields returned error: %v; output=%q", err, output)
	}
	if len(fields) != 1 {
		t.Fatalf("fields = %#v, want one field", fields)
	}
	if fields[0] != weirdValue {
		t.Fatalf("field = %q, want %q", fields[0], weirdValue)
	}
}
