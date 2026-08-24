package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/v2/internal/broker"
	"github.com/zigai/agent-sessions/v2/internal/observer"
	"github.com/zigai/agent-sessions/v2/internal/service"
	"github.com/zigai/agent-sessions/v2/pkg/brokerapi"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

type observeOptions struct {
	once     bool
	quiet    bool
	interval time.Duration
	grace    time.Duration
}

const (
	observeDefaultInterval = 300 * time.Millisecond
	realtimeComponentCount = 3
)

var errObserverRunDegraded = errors.New("observer reconciliation degraded")

func (app *application) newMonitorRunCommand() *cobra.Command {
	o := observeOptions{interval: observeDefaultInterval}
	command := &cobra.Command{
		Use:   "run",
		Short: "Observe agent processes and native sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.interval <= 0 {
				return errInvalidObserveInterval
			}
			if o.grace < 0 {
				return errInvalidObserveGracePeriod
			}
			if o.once {
				watcher := observer.New(observer.Options{
					StorePath:   app.store().Path(),
					Interval:    o.interval,
					GracePeriod: o.grace,
					HealthPath:  app.store().Path() + ".observer-health.json",
					Quiet:       o.quiet,
				})

				return app.runObserver(cmd.Context(), o, watcher)
			}

			store, err := registry.OpenMemoryStore(app.store().Path())
			if err != nil {
				return fmt.Errorf("opening in-memory registry: %w", err)
			}
			watcher := observer.New(observer.Options{
				Store:       store,
				StorePath:   store.Path(),
				Interval:    o.interval,
				GracePeriod: o.grace,
				HealthPath:  store.Path() + ".observer-health.json",
				Quiet:       o.quiet,
			})
			server := broker.New(broker.Options{
				Store:      store,
				SocketPath: brokerapi.SocketPath(store.Path()),
			})

			return app.runRealtimeObserver(cmd.Context(), o, watcher, store, server)
		},
	}
	flags := command.Flags()
	flags.BoolVar(&o.once, "once", false, "run one reconciliation cycle")
	flags.DurationVar(&o.interval, "interval", o.interval, "reconciliation interval")
	flags.DurationVar(&o.grace, "grace-period", 0, "absence grace period")
	flags.BoolVar(&o.quiet, "quiet", false, "suppress human cycle output and diagnostics")
	return command
}

type trackerComponentResult struct {
	name string
	err  error
}

func (app *application) runRealtimeObserver(
	ctx context.Context,
	options observeOptions,
	watcher *observer.Observer,
	store *registry.MemoryStore,
	server *broker.Server,
) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan trackerComponentResult, realtimeComponentCount)
	run := func(name string, operation func() error) {
		go func() {
			results <- trackerComponentResult{name: name, err: operation()}
		}()
	}
	run("broker", func() error { return server.Serve(runContext) })
	run("persistence", func() error {
		return store.RunPersistence(runContext, 0, 0)
	})
	run("observer", func() error { return app.runObserver(runContext, options, watcher) })

	first := <-results
	cancel()
	all := []trackerComponentResult{first, <-results, <-results}
	var joined error
	for _, result := range all {
		if result.err == nil {
			continue
		}
		joined = errors.Join(joined, fmt.Errorf("%s: %w", result.name, result.err))
	}
	if joined != nil {
		return joined
	}

	return nil
}

func (app *application) runObserver(ctx context.Context, options observeOptions, watcher *observer.Observer) error {
	if options.once {
		return app.runObserverOnce(ctx, options, watcher)
	}
	if !options.quiet {
		app.warnf("observer started interval=%s grace-period=%s\n", options.interval, options.grace)
	}
	handle := func(result observer.Result) error {
		if app.outputJSON {
			return app.writeJSONLine(result)
		}
		return app.writeObserverResult(result)
	}
	var err error
	if options.quiet && !app.outputJSON {
		err = watcher.Run(ctx)
	} else {
		err = watcher.RunWithResults(ctx, handle)
	}
	if err != nil {
		return fmt.Errorf("observer run: %w", err)
	}
	return nil
}

func (app *application) runObserverOnce(ctx context.Context, options observeOptions, watcher *observer.Observer) error {
	result, err := watcher.RunOnce(ctx)
	if err != nil {
		return fmt.Errorf("observer run once: %w", err)
	}
	var writeErr error
	if app.outputJSON {
		writeErr = app.writeJSON(result)
	} else if !options.quiet {
		writeErr = app.writeObserverResult(result)
	}
	if writeErr != nil {
		return writeErr
	}
	if !result.Degraded {
		return nil
	}
	if result.Error == "" {
		return errObserverRunDegraded
	}
	return fmt.Errorf("%w: %s", errObserverRunDegraded, result.Error)
}

func (app *application) writeObserverResult(result observer.Result) error {
	if err := app.writef(
		"observations=%d sessions=%d processes=%d panes=%d catalog=%d\n",
		result.Observations, result.Sessions, result.Processes, result.Panes, result.Catalog,
	); err != nil {
		return err
	}
	if err := app.writef(
		"present=%d gone=%d changed=%d degraded=%t\n",
		result.Present, result.Gone, result.Changed, result.Degraded,
	); err != nil {
		return err
	}
	if result.Error != "" {
		return app.writeHumanDetails([]humanDetail{{label: "Error", value: result.Error}})
	}
	return nil
}

type serviceOptions struct {
	binary          string
	interval, grace time.Duration
	dryRun          bool
}

var errUnknownServiceOperation = errors.New("unknown service operation")

func runServiceOperation(ctx context.Context, operation string, options service.Options) (service.Result, error) {
	var result service.Result
	var err error
	switch operation {
	case installCommandName:
		result, err = service.Install(ctx, options)
	case "update":
		result, err = service.Update(ctx, options)
	case "uninstall":
		result, err = service.Uninstall(ctx, options)
	case statusCommandName:
		result, err = service.Status(ctx, options)
	default:
		return service.Result{}, fmt.Errorf("%w: %s", errUnknownServiceOperation, operation)
	}
	if err != nil {
		return result, fmt.Errorf("service %s: %w", operation, err)
	}
	return result, nil
}

func (app *application) parseServiceOptions(options serviceOptions) (service.Options, error) {
	if options.interval <= 0 {
		return service.Options{}, errInvalidObserveInterval
	}
	if options.grace < 0 {
		return service.Options{}, errInvalidObserveGracePeriod
	}
	return service.Options{Binary: options.binary, StorePath: app.store().Path(), Interval: options.interval, GracePeriod: options.grace, DryRun: options.dryRun}, nil
}
