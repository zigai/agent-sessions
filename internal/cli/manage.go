package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/processinfo"
	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

var (
	errManageStopAllFailed = errors.New("one or more sessions failed to stop")
	errUnknownStopMethod   = errors.New("unknown stop method")
)

const stopTargetMaxAge = 30 * time.Minute

type manageResetResult struct {
	registry.ResetResult

	Path string `json:"path"`
}
type manageStopAllOptions struct {
	dryRun   bool
	signaler sessionStopSignaler
}
type sessionStopSignaler interface {
	ValidateStopTarget(ctx context.Context, session registry.Session, target stopTarget) (stopTargetValidation, error)
	SendTmuxInterrupt(ctx context.Context, paneID string) error
	SendProcessInterrupt(pid int) error
}
type (
	defaultSessionStopSignaler struct{}
	stopTargetValidation       struct {
		OK     bool
		Reason string
	}
)

type stopTarget struct {
	Method, Target string
	PID            int
}
type manageStopAllResult struct {
	Stoppable, Stopped, Skipped, Failed int
	DryRun                              bool                      `json:"dry_run,omitempty"`
	Results                             []manageStopSessionResult `json:"results,omitempty"`
}
type manageStopSessionResult struct {
	ID       string             `json:"id"`
	Harness  registry.Harness   `json:"harness"`
	Presence registry.Presence  `json:"presence"`
	Activity *registry.Activity `json:"activity"`
	Method   string             `json:"method,omitempty"`
	Target   string             `json:"target,omitempty"`
	Status   string             `json:"status"`
	Reason   string             `json:"reason,omitempty"`
	Error    string             `json:"error,omitempty"`
}

func (defaultSessionStopSignaler) ValidateStopTarget(ctx context.Context, s registry.Session, t stopTarget) (stopTargetValidation, error) {
	latest := time.Time{}
	if s.Observations.Process != nil {
		latest = s.Observations.Process.ObservedAt
	}
	if s.Observations.Tmux != nil && s.Observations.Tmux.ObservedAt.After(latest) {
		latest = s.Observations.Tmux.ObservedAt
	}
	if latest.IsZero() || time.Since(latest) > stopTargetMaxAge {
		return stopTargetValidation{Reason: "observation too old"}, nil
	}
	switch t.Method {
	case "tmux-interrupt":
		return validateTmuxStopTarget(ctx, s)
	case "pid-interrupt":
		return validateProcessStopTarget(ctx, s)
	default:
		return stopTargetValidation{}, fmt.Errorf("%w: %q", errUnknownStopMethod, t.Method)
	}
}

func (defaultSessionStopSignaler) SendTmuxInterrupt(ctx context.Context, p string) error {
	if err := tmuxctx.SendInterrupt(ctx, p); err != nil {
		return fmt.Errorf("send tmux interrupt: %w", err)
	}
	return nil
}

func (defaultSessionStopSignaler) SendProcessInterrupt(pid int) error {
	p, e := os.FindProcess(pid)
	if e != nil {
		return fmt.Errorf("find process %d: %w", pid, e)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}

func (app *application) newManageCommand() *cobra.Command {
	c := &cobra.Command{Use: "manage", Short: "Manage registry and agent sessions"}
	c.AddCommand(app.newManageResetCommand(), app.newManageStopAllCommand())
	return c
}

func (app *application) newManageResetCommand() *cobra.Command {
	return &cobra.Command{Use: "reset", Short: "Reset the registry state file", RunE: func(cmd *cobra.Command, _ []string) error {
		s := app.store()
		r, e := s.Reset(cmd.Context())
		if e != nil {
			return fmt.Errorf("resetting store: %w", e)
		}
		o := manageResetResult{ResetResult: r, Path: s.Path()}
		if app.outputJSON {
			return app.writeJSON(o)
		}
		return app.writef("cleared=%d remaining=%d path=%s\n", o.Cleared, o.Remaining, o.Path)
	}}
}

func (app *application) newManageStopAllCommand() *cobra.Command {
	o := manageStopAllOptions{}
	c := &cobra.Command{Use: "stop-all", Short: "Send graceful stop signals to live sessions", SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		r, e := app.runManageStopAll(cmd.Context(), o)
		if we := app.writeManageStopAllResult(r); we != nil {
			return we
		}
		return e
	}}
	c.Flags().BoolVar(&o.dryRun, "dry-run", false, "show sessions without sending signals")
	return c
}

func (app *application) runManageStopAll(ctx context.Context, o manageStopAllOptions) (manageStopAllResult, error) {
	if o.signaler == nil {
		o.signaler = defaultSessionStopSignaler{}
	}
	ss, e := app.store().List(ctx, registry.Filter{Presence: registry.PresenceLive})
	if e != nil {
		return manageStopAllResult{}, fmt.Errorf("list live sessions: %w", e)
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].ID < ss[j].ID })
	r := manageStopAllResult{DryRun: o.dryRun, Results: make([]manageStopSessionResult, 0, len(ss))}
	seen := map[string]bool{}
	for _, s := range ss {
		t, ok := stopTargetForSession(s)
		entry := manageStopSessionResult{ID: s.ID, Harness: s.Harness, Presence: s.Presence, Activity: s.Activity, Status: "skipped"}
		if !ok {
			entry.Reason = "no stop target"
			r.Skipped++
			r.Results = append(r.Results, entry)
			continue
		}
		entry.Method = t.Method
		entry.Target = t.Target
		k := t.Method + "\x00" + t.Target
		if seen[k] {
			entry.Reason = "duplicate target"
			r.Skipped++
			r.Results = append(r.Results, entry)
			continue
		}
		seen[k] = true
		r.Stoppable++
		if o.dryRun {
			entry.Status = "would_stop"
			r.Results = append(r.Results, entry)
			continue
		}
		v, e := o.signaler.ValidateStopTarget(ctx, s, t)
		if e != nil {
			entry.Status = "failed"
			entry.Error = e.Error()
			r.Failed++
			r.Results = append(r.Results, entry)
			continue
		}
		if !v.OK {
			entry.Reason = v.Reason
			r.Skipped++
			r.Stoppable--
			r.Results = append(r.Results, entry)
			continue
		}
		if e = sendStopSignal(ctx, o.signaler, t); e != nil {
			entry.Status = "failed"
			entry.Error = e.Error()
			r.Failed++
			r.Results = append(r.Results, entry)
			continue
		}
		entry.Status = "stopped"
		r.Stopped++
		r.Results = append(r.Results, entry)
	}
	if r.Failed > 0 {
		return r, errManageStopAllFailed
	}
	return r, nil
}

func validateTmuxStopTarget(ctx context.Context, s registry.Session) (stopTargetValidation, error) {
	panes, e := tmuxctx.ListPanes(ctx)
	if e != nil {
		return stopTargetValidation{}, fmt.Errorf("list tmux panes: %w", e)
	}
	for _, p := range panes {
		if p.Tmux.PaneID != s.Tmux.PaneID {
			continue
		}
		if !tmuxTargetMatchesSession(s.Tmux, p.Tmux) {
			return stopTargetValidation{Reason: "tmux pane identity changed"}, nil
		}
		return stopTargetValidation{OK: true}, nil
	}
	return stopTargetValidation{Reason: "tmux pane no longer exists"}, nil
}

func validateProcessStopTarget(ctx context.Context, s registry.Session) (stopTargetValidation, error) {
	if s.Process == nil || !s.Process.Complete() {
		return stopTargetValidation{Reason: "missing process start identity"}, nil
	}
	id := processinfo.StartIdentity(ctx, s.Process.PID)
	if id == "" {
		return stopTargetValidation{Reason: "process no longer exists"}, nil
	}
	if id != s.Process.StartIdentity {
		return stopTargetValidation{Reason: "process identity changed"}, nil
	}
	cmd, e := processinfo.CommandName(ctx, s.Process.PID)
	if e != nil {
		return stopTargetValidation{}, fmt.Errorf("read process command: %w", e)
	}
	if !harnessCommandMatches(s.Harness, cmd) {
		return stopTargetValidation{Reason: "process command changed"}, nil
	}
	return stopTargetValidation{OK: true}, nil
}

func tmuxTargetMatchesSession(a, b registry.TmuxContext) bool {
	if a.ServerSocket != "" && a.ServerSocket != b.ServerSocket {
		return false
	}
	if a.PanePID > 0 && b.PanePID > 0 && a.PanePID != b.PanePID {
		return false
	}
	for _, p := range [][2]string{{a.SessionID, b.SessionID}, {a.SessionName, b.SessionName}, {a.WindowID, b.WindowID}, {a.WindowIndex, b.WindowIndex}, {a.PaneIndex, b.PaneIndex}} {
		if p[0] != "" && p[1] != "" && p[0] != p[1] {
			return false
		}
	}
	return true
}

func harnessCommandMatches(h registry.Harness, c string) bool {
	base := filepath.Base(strings.TrimSpace(c))
	return slices.Contains(harnesspkg.ProcessNames(h), base)
}

func stopTargetForSession(s registry.Session) (stopTarget, bool) {
	if s.Tmux.PaneID != "" {
		return stopTarget{Method: "tmux-interrupt", Target: s.Tmux.PaneID}, true
	}
	if s.Process != nil && s.Process.PID > 0 {
		return stopTarget{Method: "pid-interrupt", Target: strconv.Itoa(s.Process.PID), PID: s.Process.PID}, true
	}
	return stopTarget{}, false
}

func sendStopSignal(ctx context.Context, s sessionStopSignaler, t stopTarget) error {
	switch t.Method {
	case "tmux-interrupt":
		if err := s.SendTmuxInterrupt(ctx, t.Target); err != nil {
			return fmt.Errorf("send tmux interrupt: %w", err)
		}
		return nil
	case "pid-interrupt":
		if err := s.SendProcessInterrupt(t.PID); err != nil {
			return fmt.Errorf("send process interrupt: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", errUnknownStopMethod, t.Method)
	}
}

func (app *application) writeManageStopAllResult(r manageStopAllResult) error {
	if app.outputJSON {
		return app.writeJSON(r)
	}
	return app.writef("stoppable=%d stopped=%d skipped=%d failed=%d\n", r.Stoppable, r.Stopped, r.Skipped, r.Failed)
}
