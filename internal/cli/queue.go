package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/reportqueue"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
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
	var leaseTimeout time.Duration

	cmd := &cobra.Command{
		Use:    drainQueueCommandName,
		Short:  "Drain queued session reports",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := app.drainQueue(cmd.Context(), reportqueue.DrainOptions{
				MaxItems:     maxItems,
				LeaseTimeout: leaseTimeout,
			})
			if err != nil {
				return err
			}
			if app.outputJSON {
				return app.writeJSON(result)
			}

			return nil
		},
	}
	cmd.Flags().IntVar(&maxItems, "max-items", 0, "maximum queue items to process")
	cmd.Flags().DurationVar(&leaseTimeout, "lease-timeout", defaultQueueLeaseTimeout, "processing lease timeout")

	return cmd
}

func (app *application) newQueueStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:    queueStatusCommandName,
		Short:  "Show queued report counts",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := reportqueue.New(app.store().Path()).Status(cmd.Context())
			if err != nil {
				return fmt.Errorf("reading report queue status: %w", err)
			}

			return app.writeJSON(status)
		},
	}
}

func (app *application) drainQueue(ctx context.Context, options reportqueue.DrainOptions) (reportqueue.DrainResult, error) {
	storePath := app.store().Path()
	queue := reportqueue.New(storePath)
	options.Processor = func(ctx context.Context, envelope reportqueue.Envelope) error {
		return app.processQueuedReport(ctx, queue, envelope)
	}

	result, err := queue.Drain(ctx, options)
	if err != nil {
		return reportqueue.DrainResult{}, fmt.Errorf("draining report queue: %w", err)
	}

	return result, nil
}

func (app *application) processQueuedReport(
	ctx context.Context,
	queue reportqueue.Queue,
	envelope reportqueue.Envelope,
) error {
	report, storePath, err := app.prepareQueuedReport(ctx, queue, envelope)
	if err != nil {
		return err
	}

	return app.storeQueuedReport(ctx, storePath, report)
}

func (app *application) prepareQueuedReport(
	ctx context.Context,
	queue reportqueue.Queue,
	envelope reportqueue.Envelope,
) (registry.Report, string, error) {
	if err := validateQueuedEnvelope(envelope); err != nil {
		return registry.Report{}, "", err
	}
	report := normalizedQueuedReport(envelope)
	if err := validateQueuedReport(report); err != nil {
		return registry.Report{}, "", err
	}
	if !envelope.NoTmux && report.Tmux.Empty() {
		report.Tmux = app.queuedReportTmux(ctx, queue, envelope)
	}

	return report, app.queuedReportStorePath(envelope), nil
}

func (app *application) queuedReportStorePath(envelope reportqueue.Envelope) string {
	storePath := strings.TrimSpace(envelope.StorePath)
	if storePath == "" {
		return app.store().Path()
	}

	return storePath
}

func validateQueuedEnvelope(envelope reportqueue.Envelope) error {
	if envelope.Version != reportqueue.EnvelopeVersion {
		return permanentQueuedReport(
			fmt.Errorf("%w: %d", errUnsupportedQueueEnvelopeVersion, envelope.Version),
		)
	}
	if envelope.Kind != reportqueue.KindReport {
		return permanentQueuedReport(
			fmt.Errorf("%w: %q", errUnsupportedQueueEnvelopeKind, envelope.Kind),
		)
	}

	return nil
}

func normalizedQueuedReport(envelope reportqueue.Envelope) registry.Report {
	report := envelope.Report.RegistryReport()
	if !envelope.RawPayloadSet && string(report.RawPayload) == "null" {
		report.RawPayload = nil
	}
	if report.ObservedAt.IsZero() {
		report.ObservedAt = envelope.CreatedAt
	}

	return report
}

func validateQueuedReport(report registry.Report) error {
	if report.Harness == "" {
		return permanentQueuedReport(registry.ErrHarnessRequired)
	}
	if report.State == "" && report.SessionID == "" && report.SessionPath == "" {
		return permanentQueuedReport(registry.ErrReportIdentityRequired)
	}

	return nil
}

func (app *application) storeQueuedReport(ctx context.Context, storePath string, report registry.Report) error {
	_, err := registry.NewFileStore(storePath).Report(ctx, report)
	if err != nil {
		if errors.Is(err, registry.ErrHarnessRequired) || errors.Is(err, registry.ErrReportIdentityRequired) {
			return permanentQueuedReport(err)
		}

		return fmt.Errorf("reporting queued session: %w", err)
	}

	return nil
}

func permanentQueuedReport(err error) error {
	if err == nil {
		return nil
	}

	return reportqueue.PermanentError{Err: err}
}

func (app *application) queuedReportTmux(
	ctx context.Context,
	queue reportqueue.Queue,
	envelope reportqueue.Envelope,
) registry.TmuxContext {
	env := tmuxctx.Env{
		TMUX:     envelope.Runtime.Env["TMUX"],
		TMUXPane: envelope.Runtime.Env["TMUX_PANE"],
	}
	minimal := tmuxctx.ContextFromEnv(env)
	if collected, err := tmuxctx.CurrentWithEnv(ctx, env); err == nil && collected != minimal {
		_ = queue.StoreTmuxContext(ctx, collected, time.Now().UTC())

		return collected
	}
	if cached, ok := queue.LookupTmuxContext(minimal, time.Now().UTC(), 0); ok {
		return cached
	}
	if !envelope.CachedTmux.Empty() {
		return envelope.CachedTmux
	}

	return minimal
}

func (app *application) kickQueueDrainer(ctx context.Context, storePath string) {
	kickQueueDrainer(ctx, storePath)
}

func kickQueueDrainer(ctx context.Context, storePath string) {
	binary, err := os.Executable()
	if err != nil || strings.TrimSpace(binary) == "" || strings.HasSuffix(binary, ".test") {
		return
	}
	args := []string{}
	if strings.TrimSpace(storePath) != "" {
		args = append(args, "--store", storePath)
	}
	args = append(args, drainQueueCommandName, "--max-items", strconv.Itoa(drainQueueBatchSize))
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = nil
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		_ = devNull.Close()
		return
	}
	_ = devNull.Close()
	go func() {
		// The kick is best-effort and has no caller left to receive a child error.
		_ = cmd.Wait()
	}()
}

func kickQueueDrainerForArgs(args []string) {
	if len(args) > 0 && (argsContain(args, drainQueueCommandName) || argsContain(args, queueStatusCommandName)) {
		return
	}

	kickQueueDrainer(context.Background(), "")
}

func argsContain(args []string, target string) bool {
	return slices.Contains(args, target)
}
