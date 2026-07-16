package tmuxctx

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestCapturePaneUsesBoundedBottomBufferAndServer(t *testing.T) {
	t.Parallel()
	var calls [][]string
	run := func(_ context.Context, _ Env, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 1 {
			return "bottom screen\n", nil
		}
		return "Codex task\n", nil
	}
	pane := Pane{Tmux: testTmuxContext("%7"), ServerIdentity: "-L:work", PanePID: 1, PaneTTY: "/dev/pts/1"}
	snapshot, err := CapturePaneWithOptions(context.Background(), pane, CaptureOptions{Run: run, Lines: 500})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Text != "bottom screen" || snapshot.Title != "Codex task" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	wantCapture := []string{"-L", "work", "capture-pane", "-p", "-J", "-e", "-S", "-100", "-t", "%7"}
	if !reflect.DeepEqual(calls[0], wantCapture) {
		t.Fatalf("capture args = %#v, want %#v", calls[0], wantCapture)
	}
}

func TestBoundBottomLinesPreservesBlankRows(t *testing.T) {
	t.Parallel()
	input := strings.Join(append([]string{"discard"}, append(make([]string, 99), "last")...), "\n") + "\n\n"
	bounded := boundBottomLines(input, 100)
	lines := strings.Split(bounded, "\n")
	if len(lines) != 100 || lines[0] != "" || lines[len(lines)-2] != "last" || lines[len(lines)-1] != "" {
		t.Fatalf("bounded lines = %#v", lines)
	}
}

var errTestTitleUnavailable = errors.New("title unavailable")

func TestCapturePaneTitleFailureDoesNotDiscardScreen(t *testing.T) {
	t.Parallel()
	calls := 0
	run := func(context.Context, Env, ...string) (string, error) {
		calls++
		if calls == 1 {
			return "screen", nil
		}
		return "", errTestTitleUnavailable
	}
	pane := Pane{Tmux: testTmuxContext("%8"), ServerIdentity: "/tmp/tmux.sock", PanePID: 1, PaneTTY: "/dev/pts/2"}
	snapshot, err := CapturePaneWithOptions(context.Background(), pane, CaptureOptions{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Text != "screen" || snapshot.Title != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func testTmuxContext(paneID string) registry.TmuxContext {
	return registry.TmuxContext{Inside: true, PaneID: paneID}
}
