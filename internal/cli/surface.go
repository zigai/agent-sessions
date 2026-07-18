package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/v2/internal/install"
	"github.com/zigai/agent-sessions/v2/internal/service"
	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

var (
	errAllWithAgents         = errors.New("all cannot be combined with agent names")
	errAgentRequired         = errors.New("at least one agent or all is required")
	errCleanSelection        = errors.New("choose exactly one of --all or --older-than")
	errNegativeCleanAge      = errors.New("older-than must be nonnegative")
	errSessionReference      = errors.New("session reference is ambiguous")
	errStopSelection         = errors.New("provide one session or --all")
	errIntegrationStatusFail = errors.New("one or more integrations could not be inspected")
	errTargetBinaryNeedsShim = errors.New("--target-binary requires --shim")
	errTargetBinaryWithAll   = errors.New("--target-binary cannot be used with all")
)

type integrationCommandOptions struct {
	binary, targetBinary string
	dryRun, force, shim  bool
}

type setupResult struct {
	Integrations []install.Result `json:"integrations"`
	Monitor      service.Result   `json:"monitor"`
}

func (app *application) newSetupCommand() *cobra.Command {
	options := integrationCommandOptions{binary: defaultInstallBinary()}
	serviceConfig := serviceOptions{binary: defaultInstallBinary(), interval: serviceDefaultInterval}
	command := &cobra.Command{
		Use:   "setup <agent...|all>",
		Short: "Connect agents and start background tracking",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			integrations, err := app.installIntegrations(cmd.Context(), args, options)
			if err != nil {
				return err
			}
			serviceConfig.binary = options.binary
			serviceConfig.dryRun = options.dryRun
			serviceOptions, err := app.parseServiceOptions(serviceConfig)
			if err != nil {
				return err
			}
			monitor, err := runServiceOperation(cmd.Context(), "update", serviceOptions)
			if err != nil {
				return fmt.Errorf("enable monitor: %w", err)
			}
			result := setupResult{Integrations: integrations, Monitor: monitor}
			if app.outputJSON {
				return app.writeJSON(result)
			}
			if err := app.writeIntegrationResults(integrations); err != nil {
				return err
			}
			return app.writef("monitor: %s\nnext: agent-sessions list\n", monitor.Message)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.binary, "binary", options.binary, "agent-sessions binary used by integrations and monitor")
	flags.BoolVar(&options.dryRun, "dry-run", false, "show changes without writing")
	flags.BoolVar(&options.force, "force", false, "replace foreign integration files")
	return command
}

func (app *application) newIntegrationsCommand() *cobra.Command {
	command := &cobra.Command{Use: integrationsCommand, Short: "Install, remove, and inspect agent integrations"}
	command.AddCommand(app.newIntegrationsInstallCommand(), app.newIntegrationsRemoveCommand(), app.newIntegrationsStatusCommand())
	return command
}

func (app *application) newIntegrationsInstallCommand() *cobra.Command {
	options := integrationCommandOptions{binary: defaultInstallBinary()}
	command := &cobra.Command{Use: installCommandName + " <agent...|all>", Short: "Install or update agent integrations", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("target-binary") && !options.shim {
			return errTargetBinaryNeedsShim
		}
		if cmd.Flags().Changed("target-binary") && len(args) == 1 && strings.EqualFold(args[0], "all") {
			return errTargetBinaryWithAll
		}
		results, err := app.installIntegrations(cmd.Context(), args, options)
		if app.outputJSON {
			if writeErr := app.writeJSON(results); writeErr != nil {
				return writeErr
			}
		} else if writeErr := app.writeIntegrationResults(results); writeErr != nil {
			return writeErr
		}
		return err
	}}
	flags := command.Flags()
	flags.StringVar(&options.binary, "binary", options.binary, "agent-sessions binary used by installed integrations")
	flags.StringVar(&options.targetBinary, "target-binary", "", "real agent binary path for shim installs")
	flags.BoolVar(&options.dryRun, "dry-run", false, "show changes without writing")
	flags.BoolVar(&options.force, "force", false, "replace a foreign integration file")
	flags.BoolVar(&options.shim, "shim", false, "install the documented process-lifetime fallback")
	return command
}

func (app *application) newIntegrationsRemoveCommand() *cobra.Command {
	options := integrationCommandOptions{}
	command := &cobra.Command{Use: "remove <agent...|all>", Short: "Remove agent-sessions-owned integrations", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		harnesses, err := selectedHarnesses(args, false)
		if err != nil {
			return err
		}
		results := make([]install.Result, 0, len(harnesses))
		var failures []error
		for _, harnessID := range harnesses {
			result, removeErr := install.RemoveContext(cmd.Context(), install.Options{Harness: harnessID, Binary: options.binary, DryRun: options.dryRun})
			if removeErr != nil {
				result = failedIntegrationResult(harnessID, "remove failed", removeErr)
				failures = append(failures, removeErr)
			}
			results = append(results, result)
		}
		if app.outputJSON {
			if writeErr := app.writeJSON(results); writeErr != nil {
				return writeErr
			}
		} else if writeErr := app.writeIntegrationResults(results); writeErr != nil {
			return writeErr
		}
		return errors.Join(failures...)
	}}
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "show changes without writing")
	return command
}

func (app *application) newIntegrationsStatusCommand() *cobra.Command {
	binary := defaultInstallBinary()
	command := &cobra.Command{Use: "status [agent...]", Short: "Show integration installation state", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return app.runIntegrationsStatus(cmd.Context(), args, binary)
	}}
	command.Flags().StringVar(&binary, "binary", binary, "expected agent-sessions binary")
	return command
}

func (app *application) runIntegrationsStatus(ctx context.Context, args []string, binary string) error {
	harnesses, err := selectedHarnesses(args, true)
	if err != nil {
		return err
	}
	results, failed := inspectIntegrationStatuses(ctx, harnesses, binary)
	if err := app.writeIntegrationStatuses(results); err != nil {
		return err
	}
	if failed {
		return errIntegrationStatusFail
	}

	return nil
}

func inspectIntegrationStatuses(ctx context.Context, harnesses []registry.Harness, binary string) ([]install.IntegrationStatus, bool) {
	results := make([]install.IntegrationStatus, 0, len(harnesses))
	failed := false
	for _, harnessID := range harnesses {
		status, err := install.InspectContext(ctx, harnessID, binary)
		if err != nil {
			failed = true
			status = install.IntegrationStatus{
				Harness:  harnessID,
				Status:   install.ArtifactForeign,
				Paths:    nil,
				Message:  err.Error(),
				NextStep: "",
			}
		}
		results = append(results, status)
	}

	return results, failed
}

func (app *application) writeIntegrationStatuses(results []install.IntegrationStatus) error {
	if app.outputJSON {
		return app.writeJSON(results)
	}
	for _, result := range results {
		if err := app.writeIntegrationStatus(result); err != nil {
			return err
		}
	}

	return nil
}

func (app *application) writeIntegrationStatus(result install.IntegrationStatus) error {
	if err := app.writef("%s\t%s\t%s\n", result.Harness, result.Status, result.Message); err != nil {
		return err
	}
	if result.NextStep != "" {
		return app.writef("  next: %s\n", result.NextStep)
	}

	return nil
}

func (app *application) installIntegrations(ctx context.Context, args []string, options integrationCommandOptions) ([]install.Result, error) {
	harnesses, err := selectedHarnesses(args, false)
	if err != nil {
		return nil, err
	}
	results := make([]install.Result, 0, len(harnesses))
	var failures []error
	for _, harnessID := range harnesses {
		result, installErr := install.RunContext(ctx, install.Options{Harness: harnessID, Binary: options.binary, TargetBinary: options.targetBinary, DryRun: options.dryRun, Force: options.force, UseShim: options.shim})
		if installErr != nil {
			result = failedIntegrationResult(harnessID, "install failed", installErr)
			failures = append(failures, installErr)
		}
		results = append(results, result)
	}
	return results, errors.Join(failures...)
}

func failedIntegrationResult(harnessID registry.Harness, message string, err error) install.Result {
	return install.Result{
		Harness:  string(harnessID),
		Path:     "",
		Changed:  false,
		Message:  message,
		NextStep: "",
		Snippet:  "",
		Error:    err.Error(),
	}
}

func (app *application) writeIntegrationResults(results []install.Result) error {
	for _, result := range results {
		if result.Error != "" {
			if err := app.writef("%s: %s\n", result.Harness, result.Error); err != nil {
				return err
			}
			continue
		}
		if err := app.writef("%s: %s (%s)\n", result.Harness, result.Message, result.Path); err != nil {
			return err
		}
		if result.NextStep != "" {
			if err := app.writef("  next: %s\n", result.NextStep); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectedHarnesses(args []string, emptyMeansAll bool) ([]registry.Harness, error) {
	if len(args) == 0 {
		if emptyMeansAll {
			return install.AllHarnesses(), nil
		}
		return nil, errAgentRequired
	}
	if len(args) > 1 {
		for _, arg := range args {
			if strings.EqualFold(arg, "all") {
				return nil, errAllWithAgents
			}
		}
	}
	if len(args) == 1 && strings.EqualFold(args[0], "all") {
		return install.AllHarnesses(), nil
	}
	seen := make(map[registry.Harness]bool)
	result := make([]registry.Harness, 0, len(args))
	for _, arg := range args {
		harnessID, err := harnesspkg.Normalize(arg)
		if err != nil {
			return nil, fmt.Errorf("normalize agent: %w", err)
		}
		if seen[harnessID] {
			continue
		}
		seen[harnessID] = true
		result = append(result, harnessID)
	}
	return result, nil
}

func (app *application) newMonitorCommand() *cobra.Command {
	command := &cobra.Command{Use: monitorCommand, Short: "Manage background process tracking"}
	run := app.newObserveCommand()
	run.Use, run.Short, run.Hidden = "run", "Run process and multiplexer reconciliation", false
	command.AddCommand(run, app.newMonitorEnableCommand(), app.newMonitorDisableCommand(), app.newMonitorStatusCommand())
	return command
}

func (app *application) newMonitorEnableCommand() *cobra.Command {
	options := serviceOptions{binary: defaultInstallBinary(), interval: serviceDefaultInterval}
	command := &cobra.Command{Use: "enable", Short: "Install, update, and start background tracking", RunE: func(cmd *cobra.Command, _ []string) error {
		parsed, err := app.parseServiceOptions(options)
		if err != nil {
			return err
		}
		result, err := runServiceOperation(cmd.Context(), "update", parsed)
		if err != nil {
			return fmt.Errorf("enable monitor: %w", err)
		}
		return app.writeServiceResult(result)
	}}
	flags := command.Flags()
	flags.StringVar(&options.binary, "binary", options.binary, "agent-sessions binary run by the monitor")
	flags.DurationVar(&options.interval, "interval", options.interval, "reconciliation interval")
	flags.DurationVar(&options.grace, "grace-period", options.grace, "absence grace period")
	flags.BoolVar(&options.dryRun, "dry-run", false, "show changes without writing")
	return command
}

func (app *application) newMonitorDisableCommand() *cobra.Command {
	dryRun := false
	command := &cobra.Command{Use: "disable", Short: "Stop and remove background tracking", RunE: func(cmd *cobra.Command, _ []string) error {
		options, err := app.parseServiceOptions(serviceOptions{binary: defaultInstallBinary(), interval: serviceDefaultInterval, dryRun: dryRun})
		if err != nil {
			return err
		}
		result, err := runServiceOperation(cmd.Context(), "uninstall", options)
		if err != nil {
			return fmt.Errorf("disable monitor: %w", err)
		}
		return app.writeServiceResult(result)
	}}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show changes without writing")
	return command
}

func (app *application) newMonitorStatusCommand() *cobra.Command {
	serviceConfig := serviceOptions{binary: defaultInstallBinary(), interval: serviceDefaultInterval}
	command := &cobra.Command{Use: statusCommandName, Short: "Show background tracking state", RunE: func(cmd *cobra.Command, _ []string) error {
		options, err := app.parseServiceOptions(serviceConfig)
		if err != nil {
			return err
		}
		result, err := runServiceOperation(cmd.Context(), statusCommandName, options)
		if err != nil {
			return fmt.Errorf("monitor status: %w", err)
		}
		return app.writeServiceResult(result)
	}}
	flags := command.Flags()
	flags.StringVar(&serviceConfig.binary, "binary", serviceConfig.binary, "expected agent-sessions binary")
	flags.DurationVar(&serviceConfig.interval, "interval", serviceConfig.interval, "expected reconciliation interval")
	flags.DurationVar(&serviceConfig.grace, "grace-period", serviceConfig.grace, "expected absence grace period")
	return command
}

func (app *application) writeServiceResult(result service.Result) error {
	if app.outputJSON {
		return app.writeJSON(result)
	}
	state := "disabled"
	if result.Installed {
		state = "enabled"
	}
	return app.writef("%s: %s (manager=%s path=%s current=%t running=%t)\n", result.Message, state, result.Manager, result.ManagedPath, result.Current, result.Running)
}

func (app *application) newRegistryCommand() *cobra.Command {
	command := &cobra.Command{Use: registryCommandName, Short: "Inspect or clean registry storage"}
	path := app.newPathCommand()
	path.Hidden = false
	reset := app.newManageResetCommand()
	command.AddCommand(path, reset, app.newRegistryCleanCommand())
	return command
}

type cleanOptions struct {
	all       bool
	olderThan time.Duration
	ageSet    bool
}

func (app *application) newRegistryCleanCommand() *cobra.Command {
	options := cleanOptions{}
	command := &cobra.Command{Use: "clean", Short: "Delete gone session records", RunE: func(cmd *cobra.Command, _ []string) error {
		options.ageSet = cmd.Flags().Changed("older-than")
		return app.runRegistryClean(cmd.Context(), options)
	}}
	command.Flags().BoolVar(&options.all, "all", false, "delete every gone session record")
	command.Flags().DurationVar(&options.olderThan, "older-than", 0, "delete gone records older than this age")
	return command
}

func (app *application) runRegistryClean(ctx context.Context, options cleanOptions) error {
	if options.all == options.ageSet {
		return errCleanSelection
	}
	if options.olderThan < 0 {
		return errNegativeCleanAge
	}
	age := options.olderThan
	if options.all {
		age = 0
	}
	result, err := app.registryStore().GC(ctx, age)
	if err != nil {
		return fmt.Errorf("clean registry: %w", err)
	}
	if app.outputJSON {
		return app.writeJSON(result)
	}
	return app.writef("deleted=%d remaining=%d\n", result.Deleted, result.Remaining)
}

func (app *application) newShowCommand() *cobra.Command {
	return &cobra.Command{Use: "show <session>", Short: "Show session details", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		session, err := app.resolveSession(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if app.outputJSON {
			return app.writeJSON(session)
		}
		return app.writeSessionDetails(session)
	}}
}

func (app *application) resolveSession(ctx context.Context, reference string) (registry.Session, error) {
	sessions, err := app.registryStore().List(ctx, registry.Filter{})
	if err != nil {
		return registry.Session{}, fmt.Errorf("list sessions: %w", err)
	}
	matches := make([]registry.Session, 0, 1)
	for _, session := range sessions {
		if session.ID == reference {
			return session, nil
		}
		if strings.HasPrefix(session.ID, reference) || session.SessionID == reference || session.SessionPath == reference {
			matches = append(matches, session)
		}
	}
	if len(matches) == 0 {
		return registry.Session{}, registry.ErrSessionNotFound
	}
	if len(matches) > 1 {
		return registry.Session{}, fmt.Errorf("%w: %q matches %d sessions", errSessionReference, reference, len(matches))
	}
	return matches[0], nil
}

func (app *application) newWatchCommand() *cobra.Command {
	options := listOptions{watch: true}
	command := &cobra.Command{Use: "watch", Short: "Stream session changes", RunE: func(cmd *cobra.Command, _ []string) error {
		filter, err := buildFilter(options)
		if err != nil {
			return err
		}
		return app.runWatch(cmd.Context(), watchOptions{filter: filter, noSnapshot: options.noSnapshot, format: options.watchFormat, formatSet: cmd.Flags().Changed("format")})
	}}
	flags := command.Flags()
	flags.StringVar(&options.harness, "agent", "", "filter by agent")
	flags.StringVar(&options.presence, "presence", "", "filter by presence")
	flags.StringVar(&options.activity, "activity", "", "filter by activity")
	flags.StringVar(&options.tmuxSession, "tmux-session", "", "filter by tmux session")
	flags.StringVar(&options.multiplexerSession, "multiplexer-session", "", "filter by multiplexer session")
	flags.BoolVar(&options.noSnapshot, "no-snapshot", false, "start with future changes only")
	flags.StringVar(&options.watchFormat, "format", "", "output format: table or plain")
	return command
}

func (app *application) newStopCommand() *cobra.Command {
	all := false
	dryRun := false
	command := &cobra.Command{Use: "stop [session]", Short: "Gracefully stop sessions", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if all == (len(args) == 1) {
			return errStopSelection
		}
		return app.runStop(cmd.Context(), args, all, dryRun)
	}}
	command.Flags().BoolVar(&all, "all", false, "stop every live session")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show targets without sending signals")
	return command
}
