package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/v2/internal/reportqueue"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

const (
	defaultQueueLeaseTimeout = 5 * time.Minute
	drainQueueBatchSize      = 100
	drainQueueCommandName    = "drain-queue"
	queueStatusCommandName   = "queue-status"
)

var (
	errUnsupportedQueueEnvelopeVersion = errors.New("unsupported queue envelope version")
	errUnsupportedQueueEnvelopeKind    = errors.New("unsupported queue envelope kind")
)

func (app *application) newDrainQueueCommand() *cobra.Command {
	var maxItems int
	var lease time.Duration
	c := &cobra.Command{Use: drainQueueCommandName, Hidden: true, RunE: func(cmd *cobra.Command, _ []string) error {
		r, e := app.drainQueue(cmd.Context(), reportqueue.DrainOptions{MaxItems: maxItems, LeaseTimeout: lease})
		if e != nil {
			return e
		}
		if app.outputJSON {
			return app.writeJSON(r)
		}
		return nil
	}}
	c.Flags().IntVar(&maxItems, "max-items", 0, "maximum queue items")
	c.Flags().DurationVar(&lease, "lease-timeout", defaultQueueLeaseTimeout, "processing lease timeout")
	return c
}

func (app *application) newQueueStatusCommand() *cobra.Command {
	return &cobra.Command{Use: queueStatusCommandName, Hidden: true, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := reportqueue.New(app.store().Path()).Status(cmd.Context())
		if e != nil {
			return fmt.Errorf("queue status: %w", e)
		}
		if app.outputJSON {
			return app.writeJSON(s)
		}
		return app.writef("pending=%d ready=%d deferred=%d processing=%d retries=%d stale-leases=%d dead=%d invalid=%d root=%s\n", s.Pending, s.Ready, s.Deferred, s.Processing, s.Retries, s.StaleLeases, s.Dead, s.Invalid, s.Root)
	}}
}

func (app *application) drainQueue(ctx context.Context, o reportqueue.DrainOptions) (reportqueue.DrainResult, error) {
	q := reportqueue.New(app.store().Path())
	o.Processor = func(c context.Context, e reportqueue.Envelope) error { return app.processQueuedReport(c, q, e) }
	r, e := q.Drain(ctx, o)
	if e != nil {
		return r, fmt.Errorf("drain queue: %w", e)
	}
	return r, nil
}

func (app *application) processQueuedReport(ctx context.Context, q reportqueue.Queue, e reportqueue.Envelope) error {
	o, path, err := app.prepareQueuedReport(ctx, q, e)
	if err != nil {
		return err
	}
	return app.storeQueuedReport(ctx, path, o)
}

func (app *application) prepareQueuedReport(ctx context.Context, q reportqueue.Queue, e reportqueue.Envelope) (registry.Observation, string, error) {
	if err := validateQueuedEnvelope(e); err != nil {
		return registry.Observation{}, "", err
	}
	o := normalizedQueuedObservation(e)
	if err := validateQueuedObservation(o); err != nil {
		return registry.Observation{}, "", err
	}
	if !e.NoTmux && o.Tmux == nil {
		t := app.queuedReportTmux(ctx, q, e)
		if !t.Empty() {
			o.Tmux = &t
		}
	}
	p := strings.TrimSpace(e.StorePath)
	if p == "" {
		p = app.store().Path()
	}
	return o, p, nil
}

func validateQueuedEnvelope(e reportqueue.Envelope) error {
	if e.Version != reportqueue.EnvelopeVersion {
		return reportqueue.PermanentError{Err: fmt.Errorf("%w: %d", errUnsupportedQueueEnvelopeVersion, e.Version)}
	}
	if e.Kind != reportqueue.KindReport {
		return reportqueue.PermanentError{Err: fmt.Errorf("%w: %q", errUnsupportedQueueEnvelopeKind, e.Kind)}
	}
	return nil
}

func normalizedQueuedObservation(e reportqueue.Envelope) registry.Observation {
	o := reportqueue.RegistryObservation(e.Report)
	if !e.RawPayloadSet && string(o.RawPayload) == "null" {
		o.RawPayload = nil
	}
	if o.ObservedAt.IsZero() {
		o.ObservedAt = e.CreatedAt
	}
	return o
}

func validateQueuedObservation(o registry.Observation) error {
	if o.Harness == "" {
		return reportqueue.PermanentError{Err: registry.ErrHarnessRequired}
	}
	if o.Identity.SessionID == "" && o.Identity.SessionPath == "" && o.Process == nil && o.Catalog == nil {
		return reportqueue.PermanentError{Err: registry.ErrObservationIdentity}
	}
	return nil
}

func (app *application) storeQueuedReport(ctx context.Context, path string, o registry.Observation) error {
	_, e := registry.NewFileStore(path).Observe(ctx, o)
	if e != nil {
		return fmt.Errorf("recording queued observation: %w", e)
	}
	return nil
}

func (app *application) queuedReportTmux(ctx context.Context, q reportqueue.Queue, e reportqueue.Envelope) registry.TmuxContext {
	env := tmuxctx.Env{TMUX: e.Runtime.Env["TMUX"], TMUXPane: e.Runtime.Env["TMUX_PANE"]}
	minimal := tmuxctx.ContextFromEnv(env)
	if c, err := tmuxctx.CurrentWithEnv(ctx, env); err == nil && c != minimal {
		return c
	}
	if c, ok := q.LookupTmuxContext(minimal, time.Now().UTC(), 0); ok {
		return c
	}
	return e.CachedTmux
}
func (app *application) kickQueueDrainer(ctx context.Context, p string) { kickQueueDrainer(ctx, p) }
func (app *application) runQueuedReport(ctx context.Context, stdin io.Reader, o reportOptions) error {
	p, e := app.prepareReport(stdin, o, reportRuntimeContext{
		processes:         reportProcessAncestors(ctx, o.pid),
		defaultObservedAt: time.Now().UTC(),
	})
	if e != nil {
		return e
	}
	if p.ignored {
		if app.outputJSON {
			return app.writeJSON(map[string]string{statusCommandName: "ignored", "harness": string(p.harness)})
		}
		return nil
	}
	now := time.Now().UTC()
	q := reportqueue.New(app.store().Path())
	runtime, cachedTmux := queuedReportRuntime(q, now, nil)
	if _, e = q.Enqueue(ctx, reportqueue.Envelope{
		Version:       reportqueue.EnvelopeVersion,
		CreatedAt:     now,
		StorePath:     app.store().Path(),
		Kind:          reportqueue.KindReport,
		Report:        reportqueue.ReportFromRegistry(p.observation),
		RawPayloadSet: len(p.observation.RawPayload) > 0,
		Stdin:         p.stdin,
		Runtime:       runtime,
		CachedTmux:    cachedTmux,
	}, reportqueue.EnqueueOptions{Now: func() time.Time { return now }}); e != nil {
		return fmt.Errorf("queueing report: %w", e)
	}
	app.kickQueueDrainer(ctx, app.store().Path())
	if app.outputJSON {
		return app.writeJSON(map[string]string{statusCommandName: "queued"})
	}
	if o.quiet {
		return nil
	}
	return app.writef("queued\n")
}

func queuedReportRuntime(q reportqueue.Queue, now time.Time, parentArgs []string) (reportqueue.RuntimeContext, registry.TmuxContext) {
	tmuxEnv := tmuxctx.Env{TMUX: os.Getenv("TMUX"), TMUXPane: os.Getenv("TMUX_PANE")}
	minimalTmux := tmuxctx.ContextFromEnv(tmuxEnv)
	cachedTmux := minimalTmux
	if cached, ok := q.LookupTmuxContext(minimalTmux, now, 0); ok {
		cachedTmux = cached
	}
	runtime := reportqueue.RuntimeContext{
		CWD:        defaultCWD(),
		ParentArgs: append([]string(nil), parentArgs...),
		Env: map[string]string{
			"TMUX":      tmuxEnv.TMUX,
			"TMUX_PANE": tmuxEnv.TMUXPane,
			"PWD":       os.Getenv("PWD"),
		},
	}
	return runtime, cachedTmux
}

func kickQueueDrainer(ctx context.Context, p string) {
	b, e := os.Executable()
	if e != nil || strings.HasSuffix(b, ".test") {
		return
	}
	a := []string{}
	if p != "" {
		a = append(a, "--store", p)
	}
	a = append(a, drainQueueCommandName, "--max-items", strconv.Itoa(drainQueueBatchSize))
	cmd := exec.CommandContext(ctx, b, a...)
	_ = cmd.Start()
}
