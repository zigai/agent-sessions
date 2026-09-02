package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zigai/aht/internal/reportqueue"
	"github.com/zigai/aht/internal/tmuxctx"
	"github.com/zigai/aht/pkg/registry"
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

//nolint:unused // Queue code is isolated from the realtime hot path for offline recovery tooling.
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
	if o.Multiplexer == nil {
		multiplexer := reportMultiplexerContextFromEnv(e.Runtime.Env)
		if !multiplexer.Empty() {
			o.Multiplexer = &multiplexer
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
	o := e.Report
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

//nolint:unused // Queue code is isolated from the realtime hot path for offline recovery tooling.
func (app *application) kickQueueDrainer(ctx context.Context) {
	go func() {
		_, _ = app.drainQueue(context.WithoutCancel(ctx), reportqueue.DrainOptions{
			MaxItems:     drainQueueBatchSize,
			LeaseTimeout: defaultQueueLeaseTimeout,
		})
	}()
}

//nolint:unused // Queue code is isolated from the realtime hot path for offline recovery tooling.
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
	envelope := queuedReportEnvelope(app.store().Path(), p, o.noTmux, now, runtime, cachedTmux)
	if _, e = q.Enqueue(ctx, envelope, reportqueue.EnqueueOptions{Now: func() time.Time { return now }}); e != nil {
		return fmt.Errorf("queueing report: %w", e)
	}
	app.kickQueueDrainer(ctx)
	if app.outputJSON {
		return app.writeJSON(map[string]string{statusCommandName: "queued"})
	}
	if o.quiet {
		return nil
	}
	return app.writef("queued\n")
}

func queuedReportEnvelope(
	storePath string,
	prepared preparedReport,
	noTmux bool,
	now time.Time,
	runtime reportqueue.RuntimeContext,
	cachedTmux registry.TmuxContext,
) reportqueue.Envelope {
	return reportqueue.Envelope{
		Version:       reportqueue.EnvelopeVersion,
		CreatedAt:     now,
		StorePath:     storePath,
		Kind:          reportqueue.KindReport,
		Report:        prepared.observation,
		RawPayloadSet: len(prepared.observation.RawPayload) > 0,
		NoTmux:        noTmux,
		Runtime:       runtime,
		CachedTmux:    cachedTmux,
	}
}

//nolint:unused // Queue code is isolated from the realtime hot path for offline recovery tooling.
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
			"TMUX":                tmuxEnv.TMUX,
			"TMUX_PANE":           tmuxEnv.TMUXPane,
			"ZELLIJ_SESSION_NAME": os.Getenv("ZELLIJ_SESSION_NAME"),
			"ZELLIJ_PANE_ID":      os.Getenv("ZELLIJ_PANE_ID"),
			"HERDR_ENV":           os.Getenv("HERDR_ENV"),
			"HERDR_SESSION":       os.Getenv("HERDR_SESSION"),
			"HERDR_SOCKET_PATH":   os.Getenv("HERDR_SOCKET_PATH"),
			"HERDR_WORKSPACE_ID":  os.Getenv("HERDR_WORKSPACE_ID"),
			"HERDR_TAB_ID":        os.Getenv("HERDR_TAB_ID"),
			"HERDR_PANE_ID":       os.Getenv("HERDR_PANE_ID"),
			"PWD":                 os.Getenv("PWD"),
		},
	}
	return runtime, cachedTmux
}
