//go:build integration

package tmuxctx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestListPanesDiscoversRealNamedServer(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux integration test")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("tmux process discovery is not supported on this platform")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	tmuxDirectory, err := shortTmuxDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmuxDirectory) })
	t.Setenv("TMUX_TMPDIR", tmuxDirectory)

	name := fmt.Sprintf("aht-discovery-%d-%d", os.Getpid(), time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = exec.CommandContext(context.Background(), "tmux", "-L", name, "kill-server").Run() }()
	if output, err := exec.CommandContext(ctx, "tmux", "-L", name, "-f", "/dev/null", "new-session", "-d", "-s", "discovery", "sleep", "30").CombinedOutput(); err != nil {
		t.Fatalf("start named tmux server: %v: %s", err, output)
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		panes, err := ListPanes(ctx)
		if err == nil {
			for _, pane := range panes {
				if pane.Tmux.SessionName != "discovery" || filepath.Base(pane.ServerIdentity) != name {
					continue
				}
				if pane.ServerIdentity == "-L:"+name || !filepath.IsAbs(pane.ServerIdentity) || pane.Tmux.ServerSocket != pane.ServerIdentity {
					t.Fatalf("named server identity was not canonical: %#v", pane)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("named tmux server was not discovered: %v", err)
		case <-ticker.C:
		}
	}
}

func shortTmuxDirectory() (string, error) {
	return os.MkdirTemp("/tmp", "aht-tmux-")
}
