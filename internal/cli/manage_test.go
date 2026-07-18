package cli

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

var errTestSignal = errors.New("signal failed")

type recordingStopSignaler struct {
	validation  stopTargetValidation
	validateErr error
	sendErr     error
	pids        []int
	panes       []string
	servers     []string
}

func (signaler *recordingStopSignaler) ValidateStopTarget(context.Context, registry.Session, stopTarget) (stopTargetValidation, error) {
	return signaler.validation, signaler.validateErr
}

func (signaler *recordingStopSignaler) SendTmuxInterrupt(_ context.Context, serverIdentity, paneID string) error {
	signaler.servers = append(signaler.servers, serverIdentity)
	signaler.panes = append(signaler.panes, paneID)
	return signaler.sendErr
}

func (signaler *recordingStopSignaler) SendProcessInterrupt(pid int) error {
	signaler.pids = append(signaler.pids, pid)
	return signaler.sendErr
}

func TestRunManageStopSessionsStopsUniqueValidatedLiveTargets(t *testing.T) {
	t.Parallel()
	signaler := &recordingStopSignaler{validation: stopTargetValidation{OK: true}}
	app := &application{}
	sessions := []registry.Session{
		{ID: "a", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Process: &registry.ProcessIdentity{PID: 101}},
		{ID: "b", Harness: registry.HarnessClaude, Presence: registry.PresenceLive, Tmux: registry.TmuxContext{ServerSocket: "-L:custom", PaneID: "%2"}},
		{ID: "c", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Process: &registry.ProcessIdentity{PID: 101}},
		{ID: "d", Harness: registry.HarnessCodex, Presence: registry.PresenceGone, Process: &registry.ProcessIdentity{PID: 202}},
		{ID: "e", Harness: registry.HarnessClaude, Presence: registry.PresenceLive, Tmux: registry.TmuxContext{ServerSocket: "-L:other", PaneID: "%2"}},
	}
	result, err := app.runManageStopSessions(context.Background(), sessions, manageStopAllOptions{signaler: signaler})
	if err != nil {
		t.Fatal(err)
	}
	requireStopSummary(t, result)
	requireStopSignals(t, signaler)
}

func requireStopSummary(t *testing.T, result manageStopAllResult) {
	t.Helper()
	if result.Stoppable != 3 || result.Stopped != 3 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("stop result = %+v", result)
	}
}

func requireStopSignals(t *testing.T, signaler *recordingStopSignaler) {
	t.Helper()
	if !slices.Equal(signaler.pids, []int{101}) || !slices.Equal(signaler.panes, []string{"%2", "%2"}) {
		t.Fatalf("signals: pids=%v panes=%v", signaler.pids, signaler.panes)
	}
	if !slices.Equal(signaler.servers, []string{"-L:custom", "-L:other"}) {
		t.Fatalf("tmux server identities = %v", signaler.servers)
	}
}

func TestTmuxStopTargetValidationChecksEveryServer(t *testing.T) {
	t.Parallel()

	session := registry.Session{Tmux: registry.TmuxContext{ServerSocket: "/tmp/correct", PaneID: "%1", PanePID: 42}}
	panes := []tmuxctx.Pane{
		{Tmux: registry.TmuxContext{ServerSocket: "/tmp/wrong", PaneID: "%1", PanePID: 41}},
		{Tmux: registry.TmuxContext{ServerSocket: "/tmp/correct", PaneID: "%1", PanePID: 42}},
	}
	if validation := tmuxStopTargetValidation(session, panes); !validation.OK {
		t.Fatalf("matching pane on later server was rejected: %#v", validation)
	}
}

func TestRunManageStopSessionsReportsSignalFailure(t *testing.T) {
	t.Parallel()
	signaler := &recordingStopSignaler{validation: stopTargetValidation{OK: true}, sendErr: errTestSignal}
	app := &application{}
	sessions := []registry.Session{{ID: "a", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Process: &registry.ProcessIdentity{PID: 101}}}
	result, err := app.runManageStopSessions(context.Background(), sessions, manageStopAllOptions{signaler: signaler})
	if !errors.Is(err, errManageStopAllFailed) || result.Failed != 1 || result.Stopped != 0 {
		t.Fatalf("stop failure result = %+v, err=%v", result, err)
	}
}
