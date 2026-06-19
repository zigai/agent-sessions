package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

const (
	storeFlag           = "--store"
	noTmuxFlag          = "--no-tmux"
	reportCommand       = "report"
	runningStateArg     = "running"
	testSessionID       = "abc"
	testTmuxSessionName = "work"
)

var errTestInterruptFailed = errors.New("interrupt failed")

func TestRootCommandHasUse(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	if cmd.Use != "agent-sessions" {
		t.Fatalf("expected root command use to be agent-sessions, got %q", cmd.Use)
	}
}

func TestHeadlessTerminalIdleReportsExited(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		harness         registry.Harness
		event           string
		attributes      map[string]string
		parentArgs      []string
		attributePrefix string
	}{
		{
			name:            "codex exec stop",
			harness:         registry.HarnessCodex,
			event:           "Stop",
			attributes:      map[string]string{"codex_hook_event": "Stop"},
			parentArgs:      []string{string(registry.HarnessCodex), "exec", "hello"},
			attributePrefix: headlessAttributePrefix(registry.HarnessCodex),
		},
		{
			name:            "grok single stop",
			harness:         registry.HarnessGrok,
			event:           "Stop",
			attributes:      map[string]string{"grok_hook_event": "stop"},
			parentArgs:      []string{string(registry.HarnessGrok), "-p", "hello"},
			attributePrefix: headlessAttributePrefix(registry.HarnessGrok),
		},
		{
			name:            "opencode run idle",
			harness:         registry.HarnessOpenCode,
			event:           "session.updated",
			attributes:      map[string]string{"opencode_event": "session.updated"},
			parentArgs:      []string{string(registry.HarnessOpenCode), "run", "hello"},
			attributePrefix: headlessAttributePrefix(registry.HarnessOpenCode),
		},
		{
			name:            "kilo run idle",
			harness:         registry.HarnessKilo,
			event:           "session.idle",
			attributes:      map[string]string{"kilo_event": "session.idle"},
			parentArgs:      []string{string(registry.HarnessKilo), "run", "hello"},
			attributePrefix: headlessAttributePrefix(registry.HarnessKilo),
		},
		{
			name:            "kimi print stop",
			harness:         registry.HarnessKimiCode,
			event:           "Stop",
			attributes:      map[string]string{"kimi_code_hook_event": "Stop"},
			parentArgs:      []string{"kimi", "--print", "hello"},
			attributePrefix: headlessAttributePrefix(registry.HarnessKimiCode),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := adjustReportStateForRuntime(
				test.harness,
				registry.StateIdle,
				test.event,
				test.attributes,
				test.parentArgs,
			)
			if state != registry.StateExited {
				t.Fatalf("adjustReportStateForRuntime() = %q, want exited", state)
			}
			if test.attributes[test.attributePrefix+"_headless"] != "true" ||
				test.attributes[test.attributePrefix+"_stop_state_override"] != "headless-parent" {
				t.Fatalf("expected headless override attributes, got %#v", test.attributes)
			}
		})
	}
}

func TestInteractiveTerminalIdleStaysIdle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		harness    registry.Harness
		event      string
		attributes map[string]string
		parentArgs []string
	}{
		{
			name:       "grok interactive stop",
			harness:    registry.HarnessGrok,
			event:      "Stop",
			attributes: map[string]string{"grok_hook_event": "stop"},
			parentArgs: []string{string(registry.HarnessGrok)},
		},
		{
			name:       "opencode interactive run",
			harness:    registry.HarnessOpenCode,
			event:      "session.idle",
			attributes: map[string]string{"opencode_event": "session.idle"},
			parentArgs: []string{string(registry.HarnessOpenCode), "run", "--interactive", "hello"},
		},
		{
			name:       "claude print stop uses session end",
			harness:    registry.HarnessClaude,
			event:      "Stop",
			attributes: map[string]string{"claude_hook_event": "Stop"},
			parentArgs: []string{string(registry.HarnessClaude), "-p", "hello"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := adjustReportStateForRuntime(
				test.harness,
				registry.StateIdle,
				test.event,
				test.attributes,
				test.parentArgs,
			)
			if state != registry.StateIdle {
				t.Fatalf("adjustReportStateForRuntime() = %q, want idle", state)
			}
		})
	}
}

func TestAgyHeadlessStopReportsExited(t *testing.T) {
	t.Parallel()

	result, ok := harnesspkg.HandleHook(
		registry.HarnessAgy,
		"Stop",
		json.RawMessage(`{"conversationId":"agy-session","workspacePaths":["/repo"],"fullyIdle":true}`),
		map[string]any{"conversationId": "agy-session", "workspacePaths": []any{"/repo"}, "fullyIdle": true},
		[]string{string(registry.HarnessAgy), "--print", "hello"},
	)
	if !ok || !result.ReportOK || result.Report.State != registry.StateExited {
		t.Fatalf("HandleHook() = %#v, %v; want exited report", result, ok)
	}
}

func TestAgyInteractiveStopStaysIdle(t *testing.T) {
	t.Parallel()

	result, ok := harnesspkg.HandleHook(
		registry.HarnessAgy,
		"Stop",
		json.RawMessage(`{"conversationId":"agy-session","workspacePaths":["/repo"],"fullyIdle":true}`),
		map[string]any{"conversationId": "agy-session", "workspacePaths": []any{"/repo"}, "fullyIdle": true},
		[]string{string(registry.HarnessAgy)},
	)
	if !ok || !result.ReportOK || result.Report.State != registry.StateIdle {
		t.Fatalf("HandleHook() = %#v, %v; want idle report", result, ok)
	}
}

func TestHeadlessArgsForHarness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		harness registry.Harness
		args    []string
		want    bool
	}{
		{name: "codex exec", harness: registry.HarnessCodex, args: []string{string(registry.HarnessCodex), "exec", "hi"}, want: true},
		{name: "codex interactive prompt", harness: registry.HarnessCodex, args: []string{string(registry.HarnessCodex), "hi"}, want: false},
		{name: "grok single short", harness: registry.HarnessGrok, args: []string{string(registry.HarnessGrok), "-p", "hi"}, want: true},
		{name: "grok prompt json", harness: registry.HarnessGrok, args: []string{string(registry.HarnessGrok), "--prompt-json={}"}, want: true},
		{name: "opencode run", harness: registry.HarnessOpenCode, args: []string{string(registry.HarnessOpenCode), "run", "hi"}, want: true},
		{name: "opencode interactive run", harness: registry.HarnessOpenCode, args: []string{string(registry.HarnessOpenCode), "run", "-i", "hi"}, want: false},
		{name: "kilo run", harness: registry.HarnessKilo, args: []string{string(registry.HarnessKilo), "run", "hi"}, want: true},
		{name: "kimi print", harness: registry.HarnessKimiCode, args: []string{"kimi", "--print", "hi"}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := headlessArgsForHarness(test.harness, test.args); got != test.want {
				t.Fatalf("headlessArgsForHarness(%q, %#v) = %v, want %v", test.harness, test.args, got, test.want)
			}
		})
	}
}

func TestReportHelpListsSupportedHarnesses(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{reportCommand, "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	got := output.String()
	for _, harness := range []string{"claude", "codex", "cursor", "kimi-code", "grok", "pi", "opencode", "agy", "kilo"} {
		if !strings.Contains(got, harness) {
			t.Fatalf("expected report help to include %s, got %q", harness, got)
		}
	}
}

func TestRootHelpShowsGenericHookOnly(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "hook") {
		t.Fatalf("expected root help to include hook command, got %q", got)
	}
	if strings.Contains(got, "agy-hook") {
		t.Fatalf("expected root help to hide agy-hook compatibility command, got %q", got)
	}
}

func TestReportRequiresHarness(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{reportCommand})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing harness") {
		t.Fatalf("expected missing harness error, got %v", err)
	}
	if !strings.Contains(err.Error(), reportExampleHarness) {
		t.Fatalf("expected report example in error, got %v", err)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected report error not to print full usage, got %q", stderr.String())
	}
}

func TestReportHarnessArgumentRequiresStateOrIdentity(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{reportCommand, "pi", noTmuxFlag})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "report for pi needs a state") {
		t.Fatalf("expected missing report state error, got %v", err)
	}
	if !strings.Contains(err.Error(), "agent-sessions report pi running") {
		t.Fatalf("expected pi running example in error, got %v", err)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected report error not to print full usage, got %q", stderr.String())
	}
}

func TestReportStateArgumentRequiresHarness(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{reportCommand, runningStateArg, noTmuxFlag})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `state "`+runningStateArg+`" needs --harness`) {
		t.Fatalf("expected state missing harness error, got %v", err)
	}
	if !strings.Contains(err.Error(), reportExampleStateFirst) {
		t.Fatalf("expected state-first example in error, got %v", err)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected report error not to print full usage, got %q", stderr.String())
	}
}

func TestReportAcceptsHarnessAndStateArguments(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{storeFlag, storePath, reportCommand, "pi", runningStateArg, noTmuxFlag})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "\tpi\trunning") {
		t.Fatalf("expected positional report output to include pi running, got %q", got)
	}
}

func TestReadCommandsAreMergedIntoList(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	for _, name := range []string{"summary", "watch"} {
		for _, command := range cmd.Commands() {
			if command.Name() == name {
				t.Fatalf("expected top-level %s command to be removed", name)
			}
		}
	}

	listCommand := findRootTestCommand(t, cmd, listCommandName)
	for _, flagName := range []string{"summary", "watch", "no-snapshot", "format"} {
		if listCommand.Flags().Lookup(flagName) == nil {
			t.Fatalf("expected list flag %s to be registered", flagName)
		}
	}
}

func TestReportAndList(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	reportCmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	reportCmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommand,
		"--harness", "codex",
		"--state", runningStateArg,
		"--session-id", testSessionID,
		noTmuxFlag,
	})
	if err := reportCmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{storeFlag, storePath, listCommandName})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	output := listOut.String()
	if !strings.Contains(output, "codex") {
		t.Fatalf("expected list output to include codex, got %q", output)
	}
	if !strings.Contains(output, runningStateArg) {
		t.Fatalf("expected list output to include running, got %q", output)
	}
	if !strings.Contains(output, "ago") && !strings.Contains(output, "just now") {
		t.Fatalf("expected list output to show relative updated time, got %q", output)
	}
	if rfc3339Pattern().MatchString(output) {
		t.Fatalf("expected default list output not to include RFC3339 timestamp, got %q", output)
	}
}

func TestManageResetCommandClearsStore(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	ctx := context.Background()
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateRunning,
		SessionID: testSessionID,
	}); err != nil {
		t.Fatalf("reporting session: %v", err)
	}

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{storeFlag, storePath, "manage", "reset"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manage reset command failed: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "cleared=1 remaining=0") || !strings.Contains(got, "path="+storePath) {
		t.Fatalf("unexpected reset output: %q", got)
	}
	sessions, err := store.List(ctx, registry.Filter{})
	if err != nil {
		t.Fatalf("listing reset store: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected reset command to clear sessions, got %#v", sessions)
	}
}

func TestManageResetCommandJSON(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{"--json", storeFlag, storePath, "manage", "reset"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("json manage reset command failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("reset command output is not JSON: %v", err)
	}
	if got["cleared"] != float64(0) || got["remaining"] != float64(0) || got["path"] != storePath {
		t.Fatalf("unexpected reset JSON output: %#v", got)
	}
}

func TestManageStopAllDryRun(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	reportStopAllTestSessions(t, store)

	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{storeFlag, storePath, "manage", "stop-all", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manage stop-all dry run failed: %v", err)
	}

	want := "stoppable=2 stopped=0 skipped=2 failed=0 dry_run=true"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("expected dry-run summary %q, got %q", want, output.String())
	}
}

func TestManageStopAllSendsSignalsAndDeduplicatesTargets(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	reportStopAllTestSessions(t, store)

	signaler := &fakeSessionStopSignaler{}
	app := &application{storePath: storePath, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	result, err := app.runManageStopAll(context.Background(), manageStopAllOptions{signaler: signaler})
	if err != nil {
		t.Fatalf("runManageStopAll returned error: %v", err)
	}
	if result.Stoppable != 2 || result.Stopped != 2 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("unexpected stop-all result: %#v", result)
	}
	if len(signaler.tmuxPanes) != 1 || signaler.tmuxPanes[0] != "%1" {
		t.Fatalf("expected one tmux interrupt for %%1, got %#v", signaler.tmuxPanes)
	}
	if len(signaler.pids) != 1 || signaler.pids[0] != 1234 {
		t.Fatalf("expected one pid interrupt for 1234, got %#v", signaler.pids)
	}
}

func TestManageStopAllReportsFailures(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	ctx := context.Background()
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessPi,
		State:     registry.StateRunning,
		SessionID: "pid",
		PID:       1234,
	}); err != nil {
		t.Fatalf("reporting pid session: %v", err)
	}

	signaler := &fakeSessionStopSignaler{pidErr: errTestInterruptFailed}
	app := &application{storePath: storePath, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	result, err := app.runManageStopAll(ctx, manageStopAllOptions{signaler: signaler})
	if !errors.Is(err, errManageStopAllFailed) {
		t.Fatalf("expected stop-all failed error, got %v", err)
	}
	if result.Stoppable != 1 || result.Stopped != 0 || result.Failed != 1 {
		t.Fatalf("unexpected failed stop-all result: %#v", result)
	}
}

func TestListSummary(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	ctx := context.Background()
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateRunning,
		SessionID: runningStateArg,
		Tmux: registry.TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     testTmuxSessionName,
			WindowID:        "",
			WindowIndex:     "",
			WindowName:      "",
			PaneID:          "",
			PaneIndex:       "",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
	}); err != nil {
		t.Fatalf("reporting running session: %v", err)
	}
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateIdle,
		SessionID: "idle",
		Tmux: registry.TmuxContext{
			Inside:          true,
			SessionID:       "$1",
			SessionName:     testTmuxSessionName,
			WindowID:        "",
			WindowIndex:     "",
			WindowName:      "",
			PaneID:          "",
			PaneIndex:       "",
			PaneCurrentPath: "",
			PanePID:         0,
			PaneTTY:         "",
			ClientTTY:       "",
		},
	}); err != nil {
		t.Fatalf("reporting idle session: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{storeFlag, storePath, listCommandName, "--summary"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list summary command failed: %v", err)
	}

	output := listOut.String()
	if !strings.Contains(output, testTmuxSessionName) || !strings.Contains(output, "1/2") {
		t.Fatalf("expected summary output to include work active/total counts, got %q", output)
	}
}

func TestWriteSummaryTableDisambiguatesDuplicateNames(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	app := &application{stdout: &output}
	err := app.writeSummaryTable([]registry.Summary{
		{
			TmuxSessionID:   "$3",
			TmuxSessionName: "kwinl",
			Exited:          1,
		},
		{
			TmuxSessionID:   "$4",
			TmuxSessionName: "kwinl",
			Total:           1,
			Idle:            1,
		},
	})
	if err != nil {
		t.Fatalf("writeSummaryTable returned error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "kwinl ($3)") || !strings.Contains(got, "kwinl ($4)") {
		t.Fatalf("expected duplicate summary labels to include tmux ids, got %q", got)
	}
}

func TestGCExplicitZeroDeletesExitedSessions(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	ctx := context.Background()
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateExited,
		SessionID: "exited",
	}); err != nil {
		t.Fatalf("reporting exited session: %v", err)
	}
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateRunning,
		SessionID: "running",
	}); err != nil {
		t.Fatalf("reporting running session: %v", err)
	}

	var noFlagOut bytes.Buffer
	noFlagCmd := NewRootCommand(&noFlagOut, &bytes.Buffer{})
	noFlagCmd.SetArgs([]string{storeFlag, storePath, "gc"})
	if err := noFlagCmd.Execute(); err != nil {
		t.Fatalf("gc command without delete-after failed: %v", err)
	}
	if !strings.Contains(noFlagOut.String(), "deleted=0 remaining=2") {
		t.Fatalf("expected no-flag gc to keep store unchanged, got %q", noFlagOut.String())
	}

	var zeroOut bytes.Buffer
	zeroCmd := NewRootCommand(&zeroOut, &bytes.Buffer{})
	zeroCmd.SetArgs([]string{storeFlag, storePath, "gc", "--delete-after", "0s"})
	if err := zeroCmd.Execute(); err != nil {
		t.Fatalf("gc command with zero delete-after failed: %v", err)
	}
	if !strings.Contains(zeroOut.String(), "deleted=1 remaining=1") {
		t.Fatalf("expected explicit zero gc to delete exited session, got %q", zeroOut.String())
	}
}

func TestScanMarksMissingTmuxPaneSessionsExited(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "state.json")
	store := registry.NewFileStore(storePath)
	stale, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessGrok,
		State:     registry.StateIdle,
		SessionID: "old-grok",
		CWD:       "/home/zigai/Projects/sesh",
		Tmux: registry.TmuxContext{
			Inside:      true,
			SessionID:   "$11",
			SessionName: "sesh",
			PaneID:      "%27",
		},
	})
	if err != nil {
		t.Fatalf("reporting stale session: %v", err)
	}

	var output bytes.Buffer
	app := &application{
		storePath: storePath,
		stdout:    &output,
		stderr:    &bytes.Buffer{},
		listTmuxPanes: func(context.Context) ([]tmuxctx.Pane, error) {
			return []tmuxctx.Pane{
				{
					Tmux: registry.TmuxContext{
						Inside:          true,
						SessionID:       "$8",
						SessionName:     "sesh",
						WindowIndex:     "1",
						PaneID:          "%15",
						PaneCurrentPath: "/home/zigai/Projects/sesh",
					},
					CurrentCommand: "codex",
				},
			}, nil
		},
	}
	if scanErr := app.runScan(ctx, scanOptions{state: string(registry.StateIdle)}); scanErr != nil {
		t.Fatalf("runScan returned error: %v", scanErr)
	}

	updated, err := store.Get(ctx, stale.ID)
	if err != nil {
		t.Fatalf("getting stale session: %v", err)
	}
	if updated.State != registry.StateExited || updated.LastEvent != "tmux-pane-missing" {
		t.Fatalf("expected missing tmux pane session to be exited, got %#v", updated)
	}

	summaries, err := store.SummaryByTmuxSession(ctx, registry.Filter{TmuxSession: "sesh"})
	if err != nil {
		t.Fatalf("summarizing sessions: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected current and exited summary rows before gc, got %#v", summaries)
	}
}

func TestListAbsoluteTime(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	reportCmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	reportCmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommand,
		"--harness", "codex",
		"--state", runningStateArg,
		"--session-id", testSessionID,
		noTmuxFlag,
	})
	if err := reportCmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}

	var listOut bytes.Buffer
	listCmd := NewRootCommand(&listOut, &bytes.Buffer{})
	listCmd.SetArgs([]string{storeFlag, storePath, listCommandName, "--absolute-time"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	output := listOut.String()
	if !rfc3339Pattern().MatchString(output) {
		t.Fatalf("expected absolute list output to include RFC3339 timestamp, got %q", output)
	}
}

func TestListRejectsModeSpecificFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "watch flag without watch",
			args: []string{storeFlag, filepath.Join(t.TempDir(), "state.json"), listCommandName, "--no-snapshot"},
		},
		{
			name: "sort with summary",
			args: []string{storeFlag, filepath.Join(t.TempDir(), "state.json"), listCommandName, "--summary", "--sort", "updated"},
		},
		{
			name: "absolute time with watch",
			args: []string{storeFlag, filepath.Join(t.TempDir(), "state.json"), listCommandName, "--watch", "--absolute-time"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			var stderr bytes.Buffer
			cmd := NewRootCommand(&output, &stderr)
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if !errors.Is(err, errInvalidListFlags) {
				t.Fatalf("expected invalid list flags error, got %v", err)
			}
		})
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
	listCmd.SetArgs([]string{storeFlag, storePath, listCommandName, "--sort", "updated", "--desc", "--absolute-time"})
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
	listCmd.SetArgs([]string{storeFlag, filepath.Join(t.TempDir(), "state.json"), listCommandName, "--sort", "nope"})
	err := listCmd.Execute()
	if err == nil {
		t.Fatal("expected invalid sort error")
	}
	if !strings.Contains(err.Error(), "invalid list sort") {
		t.Fatalf("expected invalid sort error, got %v", err)
	}
}

func findRootTestCommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()

	for _, command := range root.Commands() {
		if command.Name() == name {
			return command
		}
	}

	t.Fatalf("expected command %s to be registered", name)

	return nil
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
		storeFlag, storePath,
		reportCommand,
		"--harness", "codex",
		"--state", runningStateArg,
		"--source", "codex-hook",
		"--raw-stdin",
		"--quiet",
		noTmuxFlag,
	})
	cmd.SetIn(strings.NewReader(`{"session_id":"codex-session","transcript_path":"/home/zigai/.codex/sessions/2026/06/18/rollout.jsonl","cwd":"/tmp","hook_event_name":"UserPromptSubmit","model":"gpt-5-codex"}`))
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
	if sessions[0].SessionPath != "/home/zigai/.codex/sessions/2026/06/18/rollout.jsonl" {
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

func TestReportClaudeHookIgnoresNonClaudePayloadQuiet(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	cmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommand,
		"--harness", "claude",
		"--state", "idle",
		"--source", "claude-hook",
		"--raw-stdin",
		"--quiet",
		noTmuxFlag,
	})
	cmd.SetIn(strings.NewReader(`{"hookEventName":"stop","sessionId":"not-claude","cwd":"/repo","workspaceRoot":"/repo","promptId":"prompt","reason":"end_turn"}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}
	if reportOut.String() != "" {
		t.Fatalf("expected quiet report to suppress output, got %q", reportOut.String())
	}

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{
		Harness: registry.HarnessClaude,
	})
	if err != nil {
		t.Fatalf("listing sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no claude sessions, got %#v", sessions)
	}
}

func TestReportGrokHookPayloadQuiet(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	cmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommand,
		"--harness", "grok",
		"--state", runningStateArg,
		"--source", "grok-hook",
		"--raw-stdin",
		"--quiet",
		noTmuxFlag,
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

func TestAgyHookPreInvocationReportsRunningSession(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "PreInvocation",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"invocationNum":3}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy-hook command failed: %v", err)
	}

	var response map[string]any
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("agy-hook response is not valid JSON: %v", err)
	}
	if len(response) != 0 {
		t.Fatalf("expected empty PreInvocation response, got %#v", response)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one agy session, got %d", len(sessions))
	}
	session := sessions[0]
	if session.State != registry.StateRunning {
		t.Fatalf("expected running state, got %q", session.State)
	}
	if session.SessionID != "agy-session" {
		t.Fatalf("expected agy session id, got %q", session.SessionID)
	}
	if session.SessionPath != "/repo/.gemini/antigravity/transcript.jsonl" {
		t.Fatalf("expected transcript path, got %q", session.SessionPath)
	}
	if session.CWD != "/repo" || session.ProjectRoot != "/repo" {
		t.Fatalf("expected repo location, got cwd=%q root=%q", session.CWD, session.ProjectRoot)
	}
	if strings.Join(session.ResumeCommand, " ") != "agy --conversation agy-session" {
		t.Fatalf("expected agy resume command, got %#v", session.ResumeCommand)
	}
	if session.Attributes["agy_hook_event"] != "PreInvocation" {
		t.Fatalf("expected agy hook event attribute, got %#v", session.Attributes)
	}
}

func TestAgyHookCompatibilityCommandDelegates(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		"agy-hook",
		"--event", "PreInvocation",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","workspacePaths":["/repo"]}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy-hook compatibility command failed: %v", err)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 1 || sessions[0].State != registry.StateRunning {
		t.Fatalf("expected compatibility command to report running agy session, got %#v", sessions)
	}
}

func TestHookUnsupportedHarnessReturnsError(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&output, &stderr)
	cmd.SetArgs([]string{
		hookCommandName, string(registry.HarnessCodex),
		"--event", "Stop",
	})
	cmd.SetIn(strings.NewReader(`{"session_id":"codex-session"}`))
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected unsupported hook harness error")
	}
}

func TestAgyHookPreToolUsePermissionReportsWaiting(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "PreToolUse",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"toolCall":{"name":"ask_permission","args":{"Cwd":"/repo/pkg"}}}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy-hook command failed: %v", err)
	}

	var response map[string]string
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("agy-hook response is not valid JSON: %v", err)
	}
	if response["decision"] != "allow" {
		t.Fatalf("expected allow decision, got %#v", response)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one agy session, got %d", len(sessions))
	}
	session := sessions[0]
	if session.State != registry.StateWaiting {
		t.Fatalf("expected waiting state, got %q", session.State)
	}
	if session.CWD != "/repo/pkg" || session.ProjectRoot != "/repo" {
		t.Fatalf("expected tool cwd and workspace root, got cwd=%q root=%q", session.CWD, session.ProjectRoot)
	}
	if session.Attributes["agy_tool_name"] != "ask_permission" {
		t.Fatalf("expected agy tool attribute, got %#v", session.Attributes)
	}
}

func TestAgyHookStopFullyIdleReportsIdle(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "Stop",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"terminationReason":"model_stop","fullyIdle":true}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy-hook command failed: %v", err)
	}

	var response map[string]string
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("agy-hook response is not valid JSON: %v", err)
	}
	if response["decision"] != "" {
		t.Fatalf("expected empty stop decision, got %#v", response)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one agy session, got %d", len(sessions))
	}
	session := sessions[0]
	if session.State != registry.StateIdle {
		t.Fatalf("expected idle state, got %q", session.State)
	}
	if session.Attributes["agy_fully_idle"] != "true" {
		t.Fatalf("expected fully idle attribute, got %#v", session.Attributes)
	}
	if session.Attributes["agy_termination_reason"] != "model_stop" {
		t.Fatalf("expected termination reason attribute, got %#v", session.Attributes)
	}
}

func TestAgyHookEmptyPostToolUseDoesNotOverwriteIdleStop(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "Stop",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"terminationReason":"NO_TOOL_CALL","fullyIdle":true}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy stop hook command failed: %v", err)
	}

	output.Reset()
	cmd = NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "PostToolUse",
	})
	cmd.SetIn(strings.NewReader(`{"conversationId":"agy-session","transcriptPath":"/repo/.gemini/antigravity/transcript.jsonl","workspacePaths":["/repo"],"toolCall":null}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy empty PostToolUse hook command failed: %v", err)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 1 {
		t.Fatalf("expected one agy session, got %d", len(sessions))
	}
	session := sessions[0]
	if session.State != registry.StateIdle {
		t.Fatalf("expected idle state, got %q", session.State)
	}
	if session.LastEvent != "Stop" {
		t.Fatalf("expected Stop to remain last event, got %q", session.LastEvent)
	}
}

func TestAgyHookMalformedPayloadStillReturnsJSON(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var output bytes.Buffer
	cmd := NewRootCommand(&output, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		hookCommandName, "agy",
		"--event", "PreToolUse",
	})
	cmd.SetIn(strings.NewReader(`not json`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agy-hook command failed: %v", err)
	}

	var response map[string]string
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("agy-hook response is not valid JSON: %v", err)
	}
	if response["decision"] != "allow" {
		t.Fatalf("expected allow decision, got %#v", response)
	}

	sessions := listAgyTestSessions(t, storePath)
	if len(sessions) != 0 {
		t.Fatalf("expected malformed payload not to create sessions, got %d", len(sessions))
	}
}

func TestReportCursorHookDefaultsOnlyQuiet(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "state.json")
	var reportOut bytes.Buffer
	cmd := NewRootCommand(&reportOut, &bytes.Buffer{})
	cmd.SetArgs([]string{
		storeFlag, storePath,
		reportCommand,
		"--harness", "cursor",
		"--state", runningStateArg,
		"--source", "cursor-hook",
		"--raw-stdin-defaults-only",
		"--quiet",
		noTmuxFlag,
	})
	cmd.SetIn(strings.NewReader(`{"session_id":"cursor-session","transcript_path":"/tmp/cursor.jsonl","workspace_roots":["/repo"],"hook_event_name":"beforeSubmitPrompt","model":"gpt-5.2","cursor_version":"1.7.2","prompt":"sensitive prompt text"}`))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report command failed: %v", err)
	}
	if reportOut.String() != "" {
		t.Fatalf("expected quiet report to suppress output, got %q", reportOut.String())
	}

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{
		Harness:     registry.HarnessCursor,
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
	if sessions[0].SessionID != "cursor-session" {
		t.Fatalf("expected cursor session id, got %q", sessions[0].SessionID)
	}
	if sessions[0].SessionPath != "/tmp/cursor.jsonl" {
		t.Fatalf("expected cursor transcript path, got %q", sessions[0].SessionPath)
	}
	if sessions[0].ProjectRoot != "/repo" || sessions[0].CWD != "/repo" {
		t.Fatalf("expected cursor root/cwd from workspace roots, got root=%q cwd=%q", sessions[0].ProjectRoot, sessions[0].CWD)
	}
	if sessions[0].Attributes["cursor_hook_event"] != "beforeSubmitPrompt" {
		t.Fatalf("expected cursor hook event attribute, got %#v", sessions[0].Attributes)
	}
	if sessions[0].Attributes["cursor_model"] != "gpt-5.2" {
		t.Fatalf("expected cursor model attribute, got %#v", sessions[0].Attributes)
	}
	if len(sessions[0].RawPayload) != 0 {
		t.Fatalf("expected defaults-only report not to store raw payload, got %s", sessions[0].RawPayload)
	}
}

func TestDefaultInstallBinaryIsAbsolute(t *testing.T) {
	t.Parallel()

	got := defaultInstallBinary()
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute default install binary, got %q", got)
	}
}

func TestParseObservedAt(t *testing.T) {
	t.Parallel()

	got, err := parseObservedAt("2026-06-18T12:00:00.123456789Z")
	if err != nil {
		t.Fatalf("parseObservedAt returned error: %v", err)
	}
	want := time.Date(2026, 6, 18, 12, 0, 0, 123456789, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}

	_, invalidErr := parseObservedAt("not-a-time")
	if invalidErr == nil {
		t.Fatal("expected invalid observed-at to fail")
	}
}

func TestCompareSessionTmuxUsesNumericIndexes(t *testing.T) {
	t.Parallel()

	w10 := registry.Session{ID: "w10", Harness: registry.HarnessCodex, Tmux: registry.TmuxContext{SessionName: "work", WindowIndex: "10", PaneIndex: "1"}}
	w2 := registry.Session{ID: "w2", Harness: registry.HarnessCodex, Tmux: registry.TmuxContext{SessionName: "work", WindowIndex: "2", PaneIndex: "1"}}
	p10 := registry.Session{ID: "p10", Harness: registry.HarnessCodex, Tmux: registry.TmuxContext{SessionName: "work", WindowIndex: "2", PaneIndex: "10"}}
	p2 := registry.Session{ID: "p2", Harness: registry.HarnessCodex, Tmux: registry.TmuxContext{SessionName: "work", WindowIndex: "2", PaneIndex: "2"}}

	if cmp := compareSessionTmux(w2, w10); cmp >= 0 {
		t.Fatalf("expected window 2 before window 10, got cmp=%d", cmp)
	}
	if cmp := compareSessionTmux(p2, p10); cmp >= 0 {
		t.Fatalf("expected pane 2 before pane 10, got cmp=%d", cmp)
	}
}

func TestInstallHooksAll(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("AGENT_SESSIONS_STATE_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGY_CLI_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

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
	for _, harness := range []string{"claude", "codex", "cursor", "kimi-code", "grok", "pi", "opencode", "agy", "kilo"} {
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

func listAgyTestSessions(t *testing.T, storePath string) []registry.Session {
	t.Helper()

	sessions, err := registry.NewFileStore(storePath).List(context.Background(), registry.Filter{
		Harness:     registry.HarnessAgy,
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	})
	if err != nil {
		t.Fatalf("listing sessions: %v", err)
	}

	return sessions
}

func reportStopAllTestSessions(t *testing.T, store *registry.FileStore) {
	t.Helper()

	ctx := context.Background()
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCodex,
		State:     registry.StateRunning,
		SessionID: "tmux",
		Tmux: registry.TmuxContext{
			Inside: true,
			PaneID: "%1",
		},
	}); err != nil {
		t.Fatalf("reporting tmux session: %v", err)
	}
	// State-less reports emulate legacy duplicate pane records for stop-all deduplication.
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessClaude,
		SessionID: "duplicate-tmux",
		Tmux: registry.TmuxContext{
			Inside: true,
			PaneID: "%1",
		},
	}); err != nil {
		t.Fatalf("reporting duplicate tmux session: %v", err)
	}
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessPi,
		State:     registry.StateRunning,
		SessionID: "pid",
		PID:       1234,
	}); err != nil {
		t.Fatalf("reporting pid session: %v", err)
	}
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessCursor,
		State:     registry.StateWaiting,
		SessionID: "no-target",
	}); err != nil {
		t.Fatalf("reporting no-target session: %v", err)
	}
	if _, err := store.Report(ctx, registry.Report{
		Harness:   registry.HarnessGrok,
		State:     registry.StateExited,
		SessionID: "exited",
	}); err != nil {
		t.Fatalf("reporting exited session: %v", err)
	}
}

type fakeSessionStopSignaler struct {
	tmuxPanes []string
	pids      []int
	tmuxErr   error
	pidErr    error
}

func (s *fakeSessionStopSignaler) SendTmuxInterrupt(_ context.Context, paneID string) error {
	s.tmuxPanes = append(s.tmuxPanes, paneID)

	return s.tmuxErr
}

func (s *fakeSessionStopSignaler) SendProcessInterrupt(pid int) error {
	s.pids = append(s.pids, pid)

	return s.pidErr
}
