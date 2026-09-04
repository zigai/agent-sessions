package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextFromEnvBuildsMinimalContext(t *testing.T) {
	t.Parallel()

	ctx := ContextFromEnv(Env{TMUX: "/tmp/tmux-1000/default,123,0", TMUXPane: "%4"})
	if !ctx.Inside || ctx.ServerSocket != "/tmp/tmux-1000/default" || ctx.PaneID != "%4" {
		t.Fatalf("unexpected minimal tmux context: %#v", ctx)
	}
}

func TestCurrentWithEnvPreservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CurrentWithEnv(ctx, Env{TMUX: "/tmp/tmux/default,1,0", TMUXPane: "%1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CurrentWithEnv() error = %v, want context.Canceled", err)
	}
}

func TestTmuxCommandEnvOverridesProcessTmuxEnv(t *testing.T) {
	t.Setenv("TMUX", "old")
	t.Setenv("TMUX_PANE", "%old")

	values := tmuxCommandEnv(Env{TMUX: "new", TMUXPane: "%new"})
	var tmuxValues []string
	var paneValues []string
	for _, value := range values {
		if strings.HasPrefix(value, "TMUX=") {
			tmuxValues = append(tmuxValues, value)
		}
		if strings.HasPrefix(value, "TMUX_PANE=") {
			paneValues = append(paneValues, value)
		}
	}
	if len(tmuxValues) != 1 || tmuxValues[0] != "TMUX=new" {
		t.Fatalf("expected one replacement TMUX value, got %#v", tmuxValues)
	}
	if len(paneValues) != 1 || paneValues[0] != "TMUX_PANE=%new" {
		t.Fatalf("expected one replacement TMUX_PANE value, got %#v", paneValues)
	}
}

func TestParseCurrent(t *testing.T) {
	t.Parallel()

	ctx, err := ParseCurrent("$1\twork\t@2\t3\tapi\t%4\t1\t/home/me/project\t1234\t/dev/pts/5\t/dev/pts/1\n")
	if err != nil {
		t.Fatalf("ParseCurrent returned error: %v", err)
	}

	if !ctx.Inside {
		t.Fatal("expected tmux context to be marked inside")
	}

	if ctx.SessionName != "work" {
		t.Fatalf("expected session name work, got %q", ctx.SessionName)
	}

	if ctx.WindowIndex != "3" {
		t.Fatalf("expected window index 3, got %q", ctx.WindowIndex)
	}

	if ctx.PaneID != "%4" {
		t.Fatalf("expected pane id %%4, got %q", ctx.PaneID)
	}

	if ctx.PanePID != 1234 {
		t.Fatalf("expected pane pid 1234, got %d", ctx.PanePID)
	}

	if ctx.PaneTTY != "/dev/pts/5" {
		t.Fatalf("expected pane tty, got %q", ctx.PaneTTY)
	}
}

func TestParseCurrentAllowsTabInPaneCurrentPath(t *testing.T) {
	t.Parallel()

	ctx, err := ParseCurrent("$1\twork\t@2\t3\tapi\t%4\t1\t/home/me/dir\twith-tab\t1234\t/dev/pts/5\t/dev/pts/1\n")
	if err != nil {
		t.Fatalf("ParseCurrent returned error: %v", err)
	}
	if ctx.PaneCurrentPath != "/home/me/dir\twith-tab" {
		t.Fatalf("expected tab in pane current path, got %q", ctx.PaneCurrentPath)
	}
}

func TestParseCurrentEscapedFields(t *testing.T) {
	t.Parallel()

	output := "tmuxctx:\\$1 tmuxctx:work tmuxctx:@2 tmuxctx:3 tmuxctx:api " +
		"tmuxctx:%4 tmuxctx:1 tmuxctx:'/home/me/dir\twith-tab' " +
		"tmuxctx:1234 tmuxctx:/dev/pts/5 tmuxctx:/dev/pts/1\n"
	ctx, err := ParseCurrent(output)
	if err != nil {
		t.Fatalf("ParseCurrent returned error: %v", err)
	}
	if ctx.SessionID != "$1" || ctx.PaneCurrentPath != "/home/me/dir\twith-tab" {
		t.Fatalf("unexpected escaped tmux context: %#v", ctx)
	}
}

func TestParseCurrentUnquotedTabInField(t *testing.T) {
	t.Parallel()

	// Real tmux #{q:...} leaves raw tabs unquoted and unescaped
	output := "tmuxctx:\\$1 tmuxctx:work tmuxctx:@2 tmuxctx:3 tmuxctx:api " +
		"tmuxctx:%4 tmuxctx:1 tmuxctx:/home/me/dir\twith-tab " +
		"tmuxctx:1234 tmuxctx:/dev/pts/5 tmuxctx:/dev/pts/1\n"
	ctx, err := ParseCurrent(output)
	if err != nil {
		t.Fatalf("ParseCurrent returned error: %v", err)
	}
	if ctx.PaneCurrentPath != "/home/me/dir\twith-tab" {
		t.Fatalf("unexpected unquoted tab path: %#v", ctx.PaneCurrentPath)
	}
}

func TestParseTmuxFieldsHandlesCurrentAndLegacyDollarQuoting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "current plain dollar", output: `tmuxctx:value\ $dollar`, want: `value $dollar`},
		{name: "legacy plain dollar", output: `tmuxctx:value\ \\$dollar`, want: `value $dollar`},
		{name: "current literal backslash", output: `tmuxctx:value\ \\\$dollar`, want: `value \$dollar`},
		{name: "legacy literal backslash", output: `tmuxctx:value\ \\\\$dollar`, want: `value \$dollar`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fields, err := parseTmuxFields(test.output, 1)
			if err != nil {
				t.Fatalf("parseTmuxFields returned error: %v", err)
			}
			if len(fields) != 1 || fields[0] != test.want {
				t.Fatalf("fields = %#v, want [%q]", fields, test.want)
			}
		})
	}
}

func TestCurrentDisplayMessageArgsTargetsTmuxPane(t *testing.T) {
	got := currentDisplayMessageArgs("format", "%12")
	want := []string{"display-message", "-p", "-t", "%12", "-F", "format"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected args: got %#v want %#v", got, want)
	}
}

func TestCurrentDisplayMessageArgsWithoutPane(t *testing.T) {
	got := currentDisplayMessageArgs("format", "")
	want := []string{"display-message", "-p", "-F", "format"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected args: got %#v want %#v", got, want)
	}
}

func TestSendInterruptTargetsCustomServer(t *testing.T) {
	t.Parallel()

	var got []string
	err := sendInterrupt(context.Background(), "-L:custom", "%12", func(_ context.Context, _ Env, args ...string) (string, error) {
		got = append([]string(nil), args...)
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-L", "custom", "send-keys", "-t", "%12", "C-c"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("interrupt argv = %#v, want %#v", got, want)
	}
}

func TestParseListPanes(t *testing.T) {
	t.Parallel()

	panes, err := ParseListPanes("$1\twork\t@2\t3\tapi\t%4\t1\t/home/me/project\t1234\t/dev/pts/5\n" +
		"$1\twork\t@2\t3\tapi\t%5\t2\t/home/me/project\t1235\t/dev/pts/6\n")
	if err != nil {
		t.Fatalf("ParseListPanes returned error: %v", err)
	}

	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}

	if panes[0].PanePID != 1234 || panes[0].PaneTTY != "/dev/pts/5" {
		t.Fatalf("unexpected first pane identity: %#v", panes[0])
	}

	if panes[1].Tmux.PaneID != "%5" {
		t.Fatalf("expected second pane id %%5, got %q", panes[1].Tmux.PaneID)
	}
}

func TestParseListPanesEscapedFields(t *testing.T) {
	t.Parallel()

	panes, err := ParseListPanes("tmuxctx:\\$1 tmuxctx:work tmuxctx:@2 tmuxctx:3 tmuxctx:api " +
		"tmuxctx:%4 tmuxctx:1 tmuxctx:'/home/me/dir\twith-tab' " +
		"tmuxctx:1234 tmuxctx:/dev/pts/5\n")
	if err != nil {
		t.Fatalf("ParseListPanes returned error: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected one pane, got %d", len(panes))
	}
	if panes[0].Tmux.PaneCurrentPath != "/home/me/dir\twith-tab" ||
		panes[0].PanePID != 1234 || panes[0].PaneTTY != "/dev/pts/5" {
		t.Fatalf("unexpected escaped pane: %#v", panes[0])
	}
}

func TestServerSpecFromArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		identity string
		tmuxArgs []string
		ok       bool
	}{
		{name: "socket", args: []string{"tmux: server", "-S", "/tmp/custom"}, identity: "/tmp/custom", tmuxArgs: []string{"-S", "/tmp/custom"}, ok: true},
		{name: "named", args: []string{"tmux: server", "-L", "other"}, identity: "-L:other", tmuxArgs: []string{"-L", "other"}, ok: true},
		{name: "listed named server", args: []string{"tmux", "-L", "other", "new-session", "-d"}, identity: "-L:other", tmuxArgs: []string{"-L", "other"}, ok: true},
		{name: "other", args: []string{"bash"}, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := serverSpecFromArgs(test.args)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if got.Identity != test.identity || strings.Join(got.Args, "\x00") != strings.Join(test.tmuxArgs, "\x00") {
				t.Fatalf("server = %#v, want identity %q args %#v", got, test.identity, test.tmuxArgs)
			}
		})
	}
}

func TestListPanesWithOptionsEnumeratesCustomServers(t *testing.T) {
	t.Parallel()
	var calls [][]string
	run := func(_ context.Context, _ Env, args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		switch strings.Join(args, "\x00") {
		case "-L\x00custom\x00list-panes\x00-a\x00-F\x00" + listPanesFormat():
			return "$2\tcustom\t@2\t0\tmain\t%2\t0\t/tmp\t200\t/dev/pts/2\t/tmp/tmux-1000/custom\n", nil
		default:
			t.Fatalf("unexpected argv: %#v", args)
			return "", nil
		}
	}
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{
		Run: run,
		ServerProcesses: func(context.Context) ([]ServerProcess, error) {
			return []ServerProcess{{PID: 42, Args: []string{"tmux", "-L", "custom", "new-session", "-d"}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("ListPanesWithOptions returned error: %v", err)
	}
	if len(panes) != 1 || len(calls) != 1 {
		t.Fatalf("panes = %#v, calls = %#v", panes, calls)
	}
	if panes[0].ServerIdentity != "/tmp/tmux-1000/custom" || panes[0].Tmux.ServerSocket != "/tmp/tmux-1000/custom" || panes[0].PanePID != 200 || panes[0].PaneTTY != "/dev/pts/2" {
		t.Fatalf("custom pane identity = %#v", panes[0])
	}
}

func TestListPanesWithOptionsDoesNotProbeMissingDefaultServer(t *testing.T) {
	t.Parallel()
	called := false
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{
		Run: func(context.Context, Env, ...string) (string, error) {
			called = true
			return "", nil
		},
		ServerProcesses: func(context.Context) ([]ServerProcess, error) { return nil, nil },
	})
	if err != nil || len(panes) != 0 || called {
		t.Fatalf("no-server discovery = panes %#v, called %t, error %v", panes, called, err)
	}
}

func TestListPanesWithOptionsIgnoresUnreachableDiscoveredServer(t *testing.T) {
	t.Parallel()
	called := false
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{
		Run: func(context.Context, Env, ...string) (string, error) {
			called = true
			return "", context.Canceled
		},
		ServerProcesses: func(context.Context) ([]ServerProcess, error) {
			return []ServerProcess{{PID: 42, Args: []string{"tmux", "-S", "/tmp/stale.sock", "new-session", "-d"}}}, nil
		},
	})
	if err != nil || len(panes) != 0 || !called {
		t.Fatalf("stale-server discovery = panes %#v, called %t, error %v", panes, called, err)
	}
}

func TestListPanesWithOptionsReportsUnreachableCurrentServer(t *testing.T) {
	t.Parallel()
	const socket = "/tmp/current.sock"
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{
		Env: Env{TMUX: socket + ",123,0", TMUXPane: "%1"},
		Run: func(context.Context, Env, ...string) (string, error) {
			return "", context.Canceled
		},
		ServerProcesses: func(context.Context) ([]ServerProcess, error) { return nil, nil },
	})
	if !errors.Is(err, context.Canceled) || len(panes) != 0 {
		t.Fatalf("current-server discovery = panes %#v, error %v", panes, err)
	}
}

func TestListPanesWithOptionsDeduplicatesCanonicalSocketIdentity(t *testing.T) {
	t.Parallel()
	const socket = "/tmp/tmux-1000/default"
	output := "$1\twork\t@1\t0\tmain\t%1\t0\t/tmp\t100\t/dev/pts/1\t" + socket + "\n"
	panes, err := ListPanesWithOptions(context.Background(), ListOptions{
		Env: Env{TMUX: socket + ",100,0", TMUXPane: "%1"},
		Run: func(context.Context, Env, ...string) (string, error) { return output, nil },
		ServerProcesses: func(context.Context) ([]ServerProcess, error) {
			return []ServerProcess{{PID: 42, Args: []string{"tmux", "new-session", "-d"}}}, nil
		},
	})
	if err != nil || len(panes) != 1 || panes[0].ServerIdentity != socket {
		t.Fatalf("deduplicated panes = %#v, error %v", panes, err)
	}
}
