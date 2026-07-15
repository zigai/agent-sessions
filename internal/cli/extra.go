package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/observer"
	"github.com/zigai/agent-sessions/internal/service"
)

type observeOptions struct {
	once     bool
	quiet    bool
	interval time.Duration
	grace    time.Duration
}

const observeDefaultInterval = 3 * time.Second

func (app *application) newObserveCommand() *cobra.Command {
	o := observeOptions{interval: observeDefaultInterval}
	command := &cobra.Command{
		Use:    "observe",
		Short:  "Observe agent processes and native sessions",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.interval <= 0 {
				return errInvalidObserveInterval
			}
			if o.grace < 0 {
				return errInvalidObserveGracePeriod
			}
			watcher := observer.New(observer.Options{
				StorePath:   app.store().Path(),
				Interval:    o.interval,
				GracePeriod: o.grace,
				HealthPath:  app.store().Path() + ".observer-health.json",
				Quiet:       o.quiet,
			})
			return app.runObserver(cmd.Context(), o, watcher)
		},
	}
	flags := command.Flags()
	flags.BoolVar(&o.once, "once", false, "run one reconciliation cycle")
	flags.DurationVar(&o.interval, "interval", o.interval, "reconciliation interval")
	flags.DurationVar(&o.grace, "grace-period", 0, "absence grace period")
	flags.BoolVar(&o.quiet, "quiet", false, "suppress human cycle output and diagnostics")
	return command
}

func (app *application) runObserver(ctx context.Context, options observeOptions, watcher *observer.Observer) error {
	if options.once {
		result, err := watcher.RunOnce(ctx)
		if err != nil {
			return fmt.Errorf("observer run once: %w", err)
		}
		if app.outputJSON {
			return app.writeJSON(result)
		}
		if options.quiet {
			return nil
		}
		return app.writeln(result.String())
	}
	if !options.quiet {
		app.warnf("observer started interval=%s grace-period=%s\n", options.interval, options.grace)
	}
	handle := func(result observer.Result) error {
		if app.outputJSON {
			return app.writeJSONLine(result)
		}
		return app.writeln(result.String())
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

type serviceOptions struct {
	binary          string
	interval, grace time.Duration
	dryRun          bool
}

var errUnknownServiceOperation = errors.New("unknown service operation")

func (app *application) newServiceCommand() *cobra.Command {
	command := &cobra.Command{Use: "service", Short: "Manage observer service", Hidden: true}
	descriptions := map[string]string{
		installCommandName: "Install and start the observer service",
		"update":           "Update and start the observer service",
		"uninstall":        "Stop and remove the observer service",
		statusCommandName:  "Show observer service state",
	}
	for _, name := range []string{installCommandName, "update", "uninstall", statusCommandName} {
		commandName := name
		o := serviceOptions{binary: defaultInstallBinary(), interval: serviceDefaultInterval}
		subcommand := &cobra.Command{
			Use:   commandName,
			Short: descriptions[commandName],
			RunE: func(cmd *cobra.Command, _ []string) error {
				opts, err := app.parseServiceOptions(o)
				if err != nil {
					return err
				}
				result, err := runServiceOperation(cmd.Context(), commandName, opts)
				if err != nil {
					return err
				}
				if app.outputJSON {
					return app.writeJSON(result)
				}
				return app.writef("manager=%s path=%s version=%d installed=%t current=%t running=%t changed=%t message=%s\n", result.Manager, result.ManagedPath, result.ManagedVersion, result.Installed, result.Current, result.Running, result.Changed, result.Message)
			},
		}
		flags := subcommand.Flags()
		switch commandName {
		case installCommandName, "update":
			flags.StringVar(&o.binary, "binary", o.binary, "observer binary")
			flags.DurationVar(&o.interval, "interval", o.interval, "observer interval")
			flags.DurationVar(&o.grace, "grace-period", o.grace, "absence grace period")
			flags.BoolVar(&o.dryRun, "dry-run", false, "show changes without writing")
		case "uninstall":
			flags.BoolVar(&o.dryRun, "dry-run", false, "show changes without writing")
		case statusCommandName:
			flags.StringVar(&o.binary, "binary", o.binary, "observer binary")
			flags.DurationVar(&o.interval, "interval", o.interval, "observer interval")
			flags.DurationVar(&o.grace, "grace-period", o.grace, "absence grace period")
		}
		command.AddCommand(subcommand)
	}
	return command
}

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
