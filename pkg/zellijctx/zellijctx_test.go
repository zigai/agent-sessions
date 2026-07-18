package zellijctx

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zigai/agent-sessions/v2/pkg/muxctx"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

var (
	errUnexpectedCommand = errors.New("unexpected zellij command")
	errBinaryNotFound    = errors.New("binary not found")
)

func TestCurrentWithEnvNormalizesTerminalPaneID(t *testing.T) {
	t.Parallel()
	context := CurrentWithEnv(Env{SessionName: "work", PaneID: "7"})
	if context.Kind != registry.MultiplexerZellij || context.SessionName != "work" || context.PaneID != "terminal_7" {
		t.Fatalf("CurrentWithEnv() = %#v", context)
	}
	if got := CurrentWithEnv(Env{SessionName: "work"}); !got.Empty() {
		t.Fatalf("incomplete environment produced context: %#v", got)
	}
}

//nolint:cyclop // fixture runner validates the exact native command sequence
func TestListPanesUsesNativeJSONInventory(t *testing.T) {
	t.Parallel()
	var calls [][]string
	run := func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "list-sessions --no-formatting":
			return "work [Created 1h ago]\ndead [Created 2h ago] (EXITED - 0)\n", nil
		case "--session work action list-panes --all --json":
			return `[
				{"id":7,"is_plugin":false,"title":"Codex","exited":false,"tab_id":3,"tab_position":1,"tab_name":"agents","pane_command":"codex","pane_cwd":"/repo"},
				{"id":8,"is_plugin":true,"title":"status","exited":false},
				{"id":9,"is_plugin":false,"title":"old","exited":true}
			]`, nil
		default:
			return "", errUnexpectedCommand
		}
	}
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 {
		t.Fatalf("panes = %#v", panes)
	}
	pane := panes[0]
	if pane.Location.Kind != registry.MultiplexerZellij || pane.Location.SessionName != "work" || pane.Location.TabID != "3" || pane.Location.TabIndex != "1" || pane.Location.PaneID != "terminal_7" || pane.CWD != "/repo" || pane.Command != "codex" {
		t.Fatalf("pane = %#v", pane)
	}
	wantCalls := [][]string{{"list-sessions", "--no-formatting"}, {"--session", "work", "action", "list-panes", "--all", "--json"}}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestListPanesSkipsWhenZellijIsNotInstalled(t *testing.T) {
	t.Parallel()
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{LookPath: func(string) (string, error) {
		return "", errBinaryNotFound
	}})
	if err != nil || panes != nil {
		t.Fatalf("ListPanesWithOptions() = %#v, %v", panes, err)
	}
}

func TestCapturePaneTargetsNativePaneAndBoundsOutput(t *testing.T) {
	t.Parallel()
	var got []string
	lines := make([]string, 101)
	for index := range lines {
		lines[index] = "line"
	}
	snapshot, err := CapturePaneWithOptions(context.Background(), muxctx.Pane{
		Location: registry.MultiplexerContext{Kind: registry.MultiplexerZellij, SessionName: "work", PaneID: "terminal_7"},
		Title:    "Codex",
	}, CaptureOptions{Run: func(_ context.Context, args ...string) (string, error) {
		got = append([]string(nil), args...)
		return strings.Join(lines, "\n") + "\n", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--session", "work", "action", "dump-screen", "--pane-id", "terminal_7"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("capture args = %#v, want %#v", got, want)
	}
	if strings.Count(snapshot.Text, "\n") != 99 || snapshot.Title != "Codex" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
