package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

var (
	errManageStopAllFailed = errors.New("one or more sessions failed to stop")
	errUnknownStopMethod   = errors.New("unknown stop method")
)

type manageResetResult struct {
	registry.ResetResult

	Path string `json:"path"`
}

type manageStopAllOptions struct {
	dryRun   bool
	signaler sessionStopSignaler
}

type sessionStopSignaler interface {
	SendTmuxInterrupt(ctx context.Context, paneID string) error
	SendProcessInterrupt(pid int) error
}

type defaultSessionStopSignaler struct{}

func (defaultSessionStopSignaler) SendTmuxInterrupt(ctx context.Context, paneID string) error {
	if err := tmuxctx.SendInterrupt(ctx, paneID); err != nil {
		return fmt.Errorf("sending tmux interrupt: %w", err)
	}

	return nil
}

func (defaultSessionStopSignaler) SendProcessInterrupt(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	signalErr := process.Signal(os.Interrupt)
	if signalErr != nil {
		return fmt.Errorf("sending interrupt to process %d: %w", pid, signalErr)
	}

	return nil
}

type stopTarget struct {
	Method string
	Target string
	PID    int
}

type manageStopAllResult struct {
	Stoppable int                       `json:"stoppable"`
	Stopped   int                       `json:"stopped"`
	Skipped   int                       `json:"skipped"`
	Failed    int                       `json:"failed"`
	DryRun    bool                      `json:"dry_run,omitempty"`
	Results   []manageStopSessionResult `json:"results,omitempty"`
}

type manageStopSessionResult struct {
	ID      string           `json:"id"`
	Harness registry.Harness `json:"harness"`
	State   registry.State   `json:"state"`
	Method  string           `json:"method,omitempty"`
	Target  string           `json:"target,omitempty"`
	Status  string           `json:"status"`
	Reason  string           `json:"reason,omitempty"`
	Error   string           `json:"error,omitempty"`
}

func (app *application) newManageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage registry state and agent sessions",
	}
	cmd.AddCommand(
		app.newManageResetCommand(),
		app.newManageStopAllCommand(),
	)

	return cmd
}

func (app *application) newManageResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset the registry state file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store := app.store()
			result, err := store.Reset(cmd.Context())
			if err != nil {
				return fmt.Errorf("resetting store: %w", err)
			}

			output := manageResetResult{
				ResetResult: result,
				Path:        store.Path(),
			}
			if app.outputJSON {
				return app.writeJSON(output)
			}

			return app.writef("cleared=%d remaining=%d path=%s\n", output.Cleared, output.Remaining, output.Path)
		},
	}
}

func (app *application) newManageStopAllCommand() *cobra.Command {
	options := manageStopAllOptions{}
	cmd := &cobra.Command{
		Use:          "stop-all",
		Short:        "Send graceful stop signals to all known agent sessions",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := app.runManageStopAll(cmd.Context(), options)
			writeErr := app.writeManageStopAllResult(result)
			if writeErr != nil {
				return writeErr
			}

			return err
		},
	}
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "show sessions that would be stopped without sending signals")

	return cmd
}

func (app *application) runManageStopAll(ctx context.Context, options manageStopAllOptions) (manageStopAllResult, error) {
	if options.signaler == nil {
		options.signaler = defaultSessionStopSignaler{}
	}

	sessions, err := app.store().List(ctx, registry.Filter{})
	if err != nil {
		return manageStopAllResult{}, fmt.Errorf("listing sessions: %w", err)
	}
	sort.SliceStable(sessions, func(i int, j int) bool {
		return compareSessionID(sessions[i], sessions[j]) < 0
	})

	result := manageStopAllResult{
		DryRun:  options.dryRun,
		Results: make([]manageStopSessionResult, 0, len(sessions)),
	}
	seenTargets := make(map[string]bool)
	for _, session := range sessions {
		if session.State == registry.StateExited {
			continue
		}

		target, ok := stopTargetForSession(session)
		entry := manageStopSessionResult{
			ID:      session.ID,
			Harness: session.Harness,
			State:   session.State,
			Status:  "skipped",
		}
		if !ok {
			entry.Reason = "no stop target"
			result.Skipped++
			result.Results = append(result.Results, entry)

			continue
		}

		entry.Method = target.Method
		entry.Target = target.Target
		targetKey := target.Method + "\x00" + target.Target
		if seenTargets[targetKey] {
			entry.Reason = "duplicate target"
			result.Skipped++
			result.Results = append(result.Results, entry)

			continue
		}
		seenTargets[targetKey] = true
		result.Stoppable++

		if options.dryRun {
			entry.Status = "would_stop"
			result.Results = append(result.Results, entry)

			continue
		}

		stopErr := sendStopSignal(ctx, options.signaler, target)
		if stopErr != nil {
			entry.Status = "failed"
			entry.Error = stopErr.Error()
			result.Failed++
			result.Results = append(result.Results, entry)

			continue
		}

		entry.Status = "stopped"
		result.Stopped++
		result.Results = append(result.Results, entry)
	}

	if result.Failed > 0 {
		return result, errManageStopAllFailed
	}

	return result, nil
}

func (app *application) writeManageStopAllResult(result manageStopAllResult) error {
	if app.outputJSON {
		return app.writeJSON(result)
	}

	if result.DryRun {
		return app.writef(
			"stoppable=%d stopped=%d skipped=%d failed=%d dry_run=true\n",
			result.Stoppable,
			result.Stopped,
			result.Skipped,
			result.Failed,
		)
	}

	return app.writef(
		"stoppable=%d stopped=%d skipped=%d failed=%d\n",
		result.Stoppable,
		result.Stopped,
		result.Skipped,
		result.Failed,
	)
}

func stopTargetForSession(session registry.Session) (stopTarget, bool) {
	if session.Tmux.PaneID != "" {
		return stopTarget{
			Method: "tmux-interrupt",
			Target: session.Tmux.PaneID,
		}, true
	}
	if session.PID > 0 {
		return stopTarget{
			Method: "pid-interrupt",
			Target: strconv.Itoa(session.PID),
			PID:    session.PID,
		}, true
	}

	return stopTarget{}, false
}

func sendStopSignal(ctx context.Context, signaler sessionStopSignaler, target stopTarget) error {
	switch target.Method {
	case "tmux-interrupt":
		if err := signaler.SendTmuxInterrupt(ctx, target.Target); err != nil {
			return fmt.Errorf("sending tmux interrupt to %s: %w", target.Target, err)
		}

		return nil
	case "pid-interrupt":
		if err := signaler.SendProcessInterrupt(target.PID); err != nil {
			return fmt.Errorf("sending process interrupt to %d: %w", target.PID, err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnknownStopMethod, target.Method)
	}
}
