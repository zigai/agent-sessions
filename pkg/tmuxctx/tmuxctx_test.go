package tmuxctx

import (
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

func TestParseListPanes(t *testing.T) {
	t.Parallel()

	panes, err := ParseListPanes("$1\twork\t@2\t3\tapi\t%4\t1\t/home/me/project\t1234\t/dev/pts/5\tcodex\n" +
		"$1\twork\t@2\t3\tapi\t%5\t2\t/home/me/project\t1235\t/dev/pts/6\tzsh\n")
	if err != nil {
		t.Fatalf("ParseListPanes returned error: %v", err)
	}

	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}

	if panes[0].CurrentCommand != "codex" {
		t.Fatalf("expected first command codex, got %q", panes[0].CurrentCommand)
	}

	if panes[1].Tmux.PaneID != "%5" {
		t.Fatalf("expected second pane id %%5, got %q", panes[1].Tmux.PaneID)
	}
}

func TestParseListPanesEscapedFields(t *testing.T) {
	t.Parallel()

	panes, err := ParseListPanes("tmuxctx:\\$1 tmuxctx:work tmuxctx:@2 tmuxctx:3 tmuxctx:api " +
		"tmuxctx:%4 tmuxctx:1 tmuxctx:'/home/me/dir\twith-tab' " +
		"tmuxctx:1234 tmuxctx:/dev/pts/5 tmuxctx:codex\n")
	if err != nil {
		t.Fatalf("ParseListPanes returned error: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected one pane, got %d", len(panes))
	}
	if panes[0].Tmux.PaneCurrentPath != "/home/me/dir\twith-tab" || panes[0].CurrentCommand != "codex" {
		t.Fatalf("unexpected escaped pane: %#v", panes[0])
	}
}
