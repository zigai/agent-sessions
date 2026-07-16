package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/observer"
	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

func TestQuietLongRunningObserverStreamsRequestedJSONLines(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := &application{outputJSON: true, stdout: &stdout, stderr: &stderr}
	watcher := observer.New(observer.Options{
		Store: registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json")),
		ProcessList: func(context.Context) ([]processinfo.Process, error) {
			cancel()
			return nil, nil
		},
		PaneList:    func(context.Context) ([]tmuxctx.Pane, error) { return nil, nil },
		CatalogList: func(context.Context) ([]observer.CatalogEntry, error) { return nil, nil },
	})
	if err := app.runObserver(ctx, observeOptions{interval: time.Second, quiet: true}, watcher); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("JSON line count = %d, want 1; output=%q", len(lines), stdout.String())
	}
	var result observer.Result
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatalf("expected compact JSON line: %v; output=%q", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("quiet observer wrote diagnostics: %q", stderr.String())
	}
}
