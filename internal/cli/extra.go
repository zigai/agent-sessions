package cli

import (
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
		Use:   "observe",
		Short: "Observe agent processes and native sessions",
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
			if o.once {
				result, err := watcher.RunOnce(cmd.Context())
				if err != nil {
					return fmt.Errorf("observer run once: %w", err)
				}
				if app.outputJSON {
					return app.writeJSON(result)
				}
				if o.quiet {
					return nil
				}
				return app.writeln(result.String())
			}
			if !o.quiet {
				app.warnf("observer started interval=%s grace-period=%s\n", o.interval, o.grace)
			}
			if err := watcher.Run(cmd.Context()); err != nil {
				return fmt.Errorf("observer run: %w", err)
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&o.once, "once", false, "run one reconciliation cycle")
	flags.DurationVar(&o.interval, "interval", o.interval, "reconciliation interval")
	flags.DurationVar(&o.grace, "grace-period", 0, "absence grace period")
	flags.BoolVar(&o.quiet, "quiet", false, "suppress cycle output")
	return command
}

type serviceOptions struct {
	binary   string
	interval string
	grace    string
	dryRun   bool
}

func (app *application) newServiceCommand() *cobra.Command {
	o := serviceOptions{}
	command := &cobra.Command{Use: "service", Short: "Manage observer service"}
	for _, name := range []string{"install", "update", "uninstall", "status"} {
		commandName := name
		subcommand := &cobra.Command{
			Use: commandName,
			RunE: func(cmd *cobra.Command, _ []string) error {
				opts, err := app.parseServiceOptions(o)
				if err != nil {
					return err
				}
				var result service.Result
				switch commandName {
				case "install":
					result, err = service.Install(cmd.Context(), opts)
				case "update":
					result, err = service.Update(cmd.Context(), opts)
				case "uninstall":
					result, err = service.Uninstall(cmd.Context(), opts)
				default:
					result, err = service.Status(cmd.Context(), opts)
				}
				if err != nil {
					return fmt.Errorf("service %s: %w", commandName, err)
				}
				if app.outputJSON {
					return app.writeJSON(result)
				}
				return app.writef("manager=%s path=%s version=%d installed=%t current=%t running=%t changed=%t message=%s\n", result.Manager, result.ManagedPath, result.ManagedVersion, result.Installed, result.Current, result.Running, result.Changed, result.Message)
			},
		}
		command.AddCommand(subcommand)
	}
	flags := command.PersistentFlags()
	flags.StringVar(&o.binary, "binary", defaultInstallBinary(), "observer binary")
	flags.StringVar(&o.interval, "interval", "3s", "observer interval")
	flags.StringVar(&o.grace, "grace-period", "0s", "absence grace period")
	flags.BoolVar(&o.dryRun, "dry-run", false, "show changes without writing")
	return command
}

func (app *application) parseServiceOptions(options serviceOptions) (service.Options, error) {
	interval, err := time.ParseDuration(options.interval)
	if err != nil {
		return service.Options{}, fmt.Errorf("parse interval: %w", err)
	}
	grace, err := time.ParseDuration(options.grace)
	if err != nil {
		return service.Options{}, fmt.Errorf("parse grace period: %w", err)
	}
	if interval <= 0 {
		return service.Options{}, errInvalidObserveInterval
	}
	if grace < 0 {
		return service.Options{}, errInvalidObserveGracePeriod
	}
	return service.Options{Binary: options.binary, StorePath: app.store().Path(), Interval: interval, GracePeriod: grace, DryRun: options.dryRun}, nil
}
