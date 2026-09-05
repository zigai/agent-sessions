package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zigai/aht/internal/observer"
	"github.com/zigai/aht/internal/processinfo"
	"github.com/zigai/aht/pkg/mux"
	"github.com/zigai/aht/pkg/registry"
)

var errTestPaneList = errors.New("pane inventory unavailable")

type failingGoneCollector struct {
	err error
}

func (f failingGoneCollector) GC(context.Context, time.Duration) (registry.GCResult, error) {
	return registry.GCResult{}, f.err
}

func TestObserverReportsAutoCleanFailures(t *testing.T) {
	for _, once := range []bool{false, true} {
		t.Run(strconv.FormatBool(once), func(t *testing.T) {
			var stdout bytes.Buffer
			app := &application{stdout: &stdout}
			watcher := observer.New(observer.Options{
				Store:       registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json")),
				ProcessList: func(context.Context) ([]processinfo.Process, error) { return nil, nil },
				PaneList:    func(context.Context) ([]mux.Pane, error) { return nil, nil },
				CatalogList: func(context.Context) ([]observer.CatalogEntry, error) { return nil, nil },
			})
			err := app.runObserver(t.Context(), observeOptions{once: once, autoClean: true, store: failingGoneCollector{err: os.ErrPermission}}, watcher)
			if !errors.Is(err, os.ErrPermission) || stdout.Len() != 0 {
				t.Fatalf("cleanup failure = %v, output = %q", err, stdout.String())
			}
		})
	}
}

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
		PaneList:    func(context.Context) ([]mux.Pane, error) { return nil, nil },
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

func TestRunObserverOnceReturnsDegradedErrorAfterWritingResult(t *testing.T) {
	t.Parallel()
	for _, outputJSON := range []bool{false, true} {
		var stdout bytes.Buffer
		app := &application{outputJSON: outputJSON, stdout: &stdout, stderr: &bytes.Buffer{}}
		watcher := observer.New(observer.Options{
			Store:       registry.NewFileStore(filepath.Join(t.TempDir(), "sessions.json")),
			ProcessList: func(context.Context) ([]processinfo.Process, error) { return nil, nil },
			PaneList:    func(context.Context) ([]mux.Pane, error) { return nil, errTestPaneList },
			CatalogList: func(context.Context) ([]observer.CatalogEntry, error) { return nil, nil },
		})
		err := app.runObserver(context.Background(), observeOptions{once: true}, watcher)
		if !errors.Is(err, errObserverRunDegraded) {
			t.Fatalf("outputJSON=%t error = %v", outputJSON, err)
		}
		if outputJSON {
			var result observer.Result
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || !result.Degraded || result.Error != errTestPaneList.Error() {
				t.Fatalf("JSON degraded result = %q, %#v, %v", stdout.String(), result, err)
			}
			continue
		}
		if !strings.Contains(stdout.String(), "degraded=true") || !strings.Contains(stdout.String(), errTestPaneList.Error()) {
			t.Fatalf("human degraded result = %q", stdout.String())
		}
	}
}
