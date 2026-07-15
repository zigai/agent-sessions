package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/zigai/agent-sessions/pkg/registry"
)

var errTestSignal = errors.New("signal failed")

type recordingStopSignaler struct {
	validation  stopTargetValidation
	validateErr error
	sendErr     error
	pids        []int
	panes       []string
}

func (signaler *recordingStopSignaler) ValidateStopTarget(context.Context, registry.Session, stopTarget) (stopTargetValidation, error) {
	return signaler.validation, signaler.validateErr
}

func (signaler *recordingStopSignaler) SendTmuxInterrupt(_ context.Context, paneID string) error {
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
		{ID: "b", Harness: registry.HarnessClaude, Presence: registry.PresenceLive, Tmux: registry.TmuxContext{PaneID: "%2"}},
		{ID: "c", Harness: registry.HarnessCodex, Presence: registry.PresenceLive, Process: &registry.ProcessIdentity{PID: 101}},
		{ID: "d", Harness: registry.HarnessCodex, Presence: registry.PresenceGone, Process: &registry.ProcessIdentity{PID: 202}},
	}
	result, err := app.runManageStopSessions(context.Background(), sessions, manageStopAllOptions{signaler: signaler})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stoppable != 2 || result.Stopped != 2 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("stop result = %+v", result)
	}
	if len(signaler.pids) != 1 || signaler.pids[0] != 101 || len(signaler.panes) != 1 || signaler.panes[0] != "%2" {
		t.Fatalf("signals: pids=%v panes=%v", signaler.pids, signaler.panes)
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
