package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/reportqueue"
	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	errInvalidAttribute = errors.New("invalid attribute")
	errInvalidListSort  = errors.New("invalid list sort")
	errInvalidListFlags = errors.New("invalid list flags")

	errInvalidReportArguments = errors.New("invalid report arguments")
	errUnexpectedReportArg    = errors.New("unexpected report argument")
	errMissingReportHarness   = errors.New("missing harness")
	errMissingReportIdentity  = errors.New("missing report state or identity")
)

const (
	tabPadding    = 2
	maxReportArgs = 2
)

const (
	reportStatesLabel        = "idle, running, waiting, unknown, exited"
	reportExampleHarness     = "agent-sessions report pi running"
	reportExampleStateFirst  = "agent-sessions report running --harness pi"
	reportExampleWithSession = "agent-sessions report --harness codex --state waiting --session-id abc"
)

const (
	secondsPerMinute = 60
	minutesPerHour   = 60
	hoursPerDay      = 24
	daysPerWeek      = 7
	daysPerYear      = 365
)

const (
	listCommandName            = "list"
	reportCommandName          = "report"
	listSortUpdated            = "updated"
	defaultStaleOwnerlessAfter = 5 * time.Minute
)

const (
	hookCommandName = "hook"
	hookConfidence  = "hook"
)

type application struct {
	storePath     string
	outputJSON    bool
	stdout        io.Writer
	stderr        io.Writer
	listTmuxPanes func(context.Context) ([]tmuxctx.Pane, error)
}

func NewRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	app := &application{
		stdout: stdout,
		stderr: stderr,
	}

	var showVersion bool
	root := &cobra.Command{
		Use:   "agent-sessions",
		Short: "Track local coding-agent sessions across harnesses and tmux panes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return app.writef("agent-sessions %s (commit: %s, built: %s)\n", version, commit, date)
			}

			return cmd.Help()
		},
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&app.storePath, "store", "", "registry state file path")
	root.PersistentFlags().BoolVar(&app.outputJSON, "json", false, "emit JSON")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")

	root.AddCommand(
		app.newReportCommand(),
		app.newListCommand(),
		app.newGetCommand(),
		app.newScanCommand(),
		app.newGCCommand(),
		app.newManageCommand(),
		app.newPathCommand(),
		app.newInstallHooksCommand(),
		app.newHookCommand(),
		app.newAgyHookCommand(),
		app.newDrainQueueCommand(),
		app.newQueueStatusCommand(),
	)

	return root
}

func Execute() {
	kickQueueDrainerForArgs(os.Args[1:])
	if handled, err := tryExecuteFastPath(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		return
	}

	cmd := NewRootCommand(os.Stdout, os.Stderr)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func (app *application) store() *registry.FileStore {
	return registry.NewFileStore(app.storePath)
}

func (app *application) registryStore() registry.Store {
	return app.store()
}

func (app *application) writeJSON(value any) error {
	encoder := json.NewEncoder(app.stdout)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(value)
	if err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

func (app *application) writef(format string, args ...any) error {
	_, err := fmt.Fprintf(app.stdout, format, args...)
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

func (app *application) writeln(args ...any) error {
	_, err := fmt.Fprintln(app.stdout, args...)
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

func (app *application) newPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the registry state file path",
		RunE: func(_ *cobra.Command, _ []string) error {
			if app.outputJSON {
				return app.writeJSON(map[string]string{"path": app.store().Path()})
			}

			return app.writeln(app.store().Path())
		},
	}
}

type reportOptions struct {
	harness         string
	state           string
	sessionID       string
	sessionPath     string
	cwd             string
	cwdAuto         bool
	projectRoot     string
	projectRootAuto bool
	pid             int
	ppid            int
	tty             string
	source          string
	confidence      string
	event           string
	observedAt      string
	attributes      []string
	rawStdin        bool
	rawDefaultsOnly bool
	noTmux          bool
	queue           bool
	quiet           bool
	resumeCommand   []string
}

type preparedReport struct {
	harness registry.Harness
	report  registry.Report
	stdin   []byte
	ignored bool
}

type reportRuntimeContext struct {
	tmux              registry.TmuxContext
	parentArgs        []string
	defaultObservedAt time.Time
}

func (app *application) newReportCommand() *cobra.Command {
	options := defaultReportOptionsFromEnv()

	cmd := &cobra.Command{
		Use:          reportCommandName + " [harness] [state]",
		Short:        "Upsert a session report from a harness hook or wrapper",
		SilenceUsage: true,
		Example: strings.Join([]string{
			"  " + reportExampleHarness,
			"  " + reportExampleStateFirst,
			"  " + reportExampleWithSession,
		}, "\n"),
		Args: cobra.MaximumNArgs(maxReportArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyReportArgs(args, &options); err != nil {
				return err
			}
			if cmd.Flags().Changed("cwd") {
				options.cwdAuto = false
			}
			if cmd.Flags().Changed("project-root") {
				options.projectRootAuto = false
			}

			return app.runReport(cmd.Context(), cmd.InOrStdin(), options)
		},
	}
	cmd.Flags().StringVar(&options.harness, "harness", options.harness, "harness name: "+strings.Join(harnesspkg.SupportedNames(), ", "))
	cmd.Flags().StringVar(&options.state, "state", options.state, "state: "+reportStatesLabel)
	cmd.Flags().StringVar(&options.sessionID, "session-id", options.sessionID, "harness session id")
	cmd.Flags().StringVar(&options.sessionPath, "session-path", options.sessionPath, "harness session file path")
	cmd.Flags().StringVar(&options.cwd, "cwd", options.cwd, "agent current working directory")
	cmd.Flags().StringVar(&options.projectRoot, "project-root", options.projectRoot, "project root")
	cmd.Flags().IntVar(&options.pid, "pid", options.pid, "agent process id")
	cmd.Flags().IntVar(&options.ppid, "ppid", options.ppid, "agent parent process id")
	cmd.Flags().StringVar(&options.tty, "tty", options.tty, "agent tty")
	cmd.Flags().StringVar(&options.source, "source", options.source, "report source label")
	cmd.Flags().StringVar(&options.confidence, "confidence", options.confidence, "confidence label")
	cmd.Flags().StringVar(&options.event, "event", options.event, "native harness event name")
	cmd.Flags().StringVar(&options.observedAt, "observed-at", options.observedAt, "RFC3339 timestamp when the harness event was observed")
	cmd.Flags().StringArrayVar(&options.attributes, "attribute", nil, "extra key=value attribute")
	cmd.Flags().StringArrayVar(&options.resumeCommand, "resume-command", nil, "explicit resume command argv item, repeatable")
	cmd.Flags().BoolVar(&options.rawStdin, "raw-stdin", false, "store stdin as raw hook payload")
	cmd.Flags().BoolVar(&options.rawDefaultsOnly, "raw-stdin-defaults-only", false, "read stdin for harness defaults without storing raw hook payload")
	cmd.Flags().BoolVar(&options.noTmux, "no-tmux", false, "do not auto-collect current tmux pane context")
	cmd.Flags().BoolVar(&options.queue, "queue", false, "durably queue the report for asynchronous registry update")
	_ = cmd.Flags().MarkHidden("queue")
	cmd.Flags().BoolVar(&options.quiet, "quiet", false, "suppress normal report output")

	return cmd
}

func defaultReportOptionsFromEnv() reportOptions {
	options := reportOptions{
		harness:     firstEnv("AGENT_SESSIONS_HARNESS", "AGENT_HARNESS"),
		state:       firstEnv("AGENT_SESSIONS_STATE", "AGENT_STATE"),
		sessionID:   firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionID)...),
		sessionPath: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionPath)...),
		cwdAuto:     true,
		projectRoot: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvProjectRoot)...),
		pid:         firstEnvInt(harnesspkg.EnvNames(harnesspkg.EnvPID)...),
		ppid:        firstEnvInt("AGENT_SESSIONS_PPID", "AGENT_PPID"),
		tty:         firstEnv("AGENT_SESSIONS_TTY", "TTY"),
		source:      firstEnv("AGENT_SESSIONS_SOURCE"),
		confidence:  firstEnv("AGENT_SESSIONS_CONFIDENCE"),
		event:       firstEnv(harnesspkg.EnvNames(harnesspkg.EnvEvent)...),
	}
	options.projectRootAuto = options.projectRoot == ""

	return options
}

func applyReportArgs(args []string, options *reportOptions) error {
	switch len(args) {
	case 0:
		return nil
	case 1:
		return applySingleReportArg(args[0], options)
	case maxReportArgs:
		return applyReportHarnessStateArgs(args[0], args[1], options)
	default:
		return nil
	}
}

func applySingleReportArg(arg string, options *reportOptions) error {
	if options.harness == "" {
		if _, err := harnesspkg.Normalize(arg); err == nil {
			options.harness = arg

			return nil
		}
	}
	if options.state == "" {
		options.state = arg

		return nil
	}

	return fmt.Errorf("%w: %q; state is already set to %q", errUnexpectedReportArg, arg, options.state)
}

func applyReportHarnessStateArgs(first string, second string, options *reportOptions) error {
	firstArg := classifyReportArg(first)
	secondArg := classifyReportArg(second)

	if options.harness == "" && options.state == "" && applyReportArgPair(firstArg, secondArg, options) {
		return nil
	}
	if options.harness == "" && firstArg.harnessOK {
		options.harness = string(firstArg.harness)

		return applySingleReportArg(second, options)
	}
	if options.state == "" && secondArg.stateOK {
		options.state = string(secondArg.state)

		return applySingleReportArg(first, options)
	}

	return fmt.Errorf("%w: %q %q; use --harness and --state explicitly", errInvalidReportArguments, first, second)
}

func applyReportArgPair(first reportArgClassification, second reportArgClassification, options *reportOptions) bool {
	if first.harnessOK && second.stateOK {
		options.harness = string(first.harness)
		options.state = string(second.state)

		return true
	}
	if first.stateOK && second.harnessOK {
		options.harness = string(second.harness)
		options.state = string(first.state)

		return true
	}

	return false
}

type reportArgClassification struct {
	harness   registry.Harness
	harnessOK bool
	state     registry.State
	stateOK   bool
}

func classifyReportArg(arg string) reportArgClassification {
	harness, harnessErr := harnesspkg.Normalize(arg)
	state, stateErr := registry.NormalizeState(arg)

	return reportArgClassification{
		harness:   harness,
		harnessOK: harnessErr == nil,
		state:     state,
		stateOK:   stateErr == nil && state != "",
	}
}

func (app *application) runReport(ctx context.Context, stdin io.Reader, options reportOptions) error {
	if options.queue {
		return app.runQueuedReport(ctx, stdin, options)
	}

	prepared, err := app.prepareReport(stdin, options, reportRuntimeContext{
		tmux:       reportTmuxContext(ctx, options.noTmux),
		parentArgs: parentProcessArgs(ctx),
	})
	if err != nil {
		return err
	}
	if prepared.ignored {
		return app.writeIgnoredReport(prepared.harness, options.quiet)
	}

	session, err := app.registryStore().Report(ctx, prepared.report)
	if err != nil {
		return fmt.Errorf("reporting session: %w", err)
	}

	return app.writeReportResult(session, options.quiet)
}

func (app *application) prepareReport(
	stdin io.Reader,
	options reportOptions,
	runtime reportRuntimeContext,
) (preparedReport, error) {
	harness, state, err := normalizeReportHarnessAndState(options)
	if err != nil {
		return preparedReport{}, err
	}

	attributes, err := parseAttributes(options.attributes)
	if err != nil {
		return preparedReport{}, err
	}

	rawPayload, defaultsPayload, stdinData, err := readStdinPayloadData(stdin, options.rawStdin, options.rawDefaultsOnly)
	if err != nil {
		return preparedReport{}, err
	}
	if !harnesspkg.PayloadCompatibleWithHarness(harness, defaultsPayload) {
		return preparedReport{harness: harness, stdin: stdinData, ignored: true}, nil
	}
	applyPayloadDefaults(&options, attributes, harnesspkg.DefaultsFromPayload(harness, defaultsPayload))
	applyReportRuntimeDefaults(&options)
	if err := requireReportIdentity(state, options); err != nil {
		return preparedReport{}, missingReportIdentityError(harness)
	}
	observedAt, err := parseObservedAt(options.observedAt)
	if err != nil {
		return preparedReport{}, err
	}
	if observedAt.IsZero() && !runtime.defaultObservedAt.IsZero() {
		observedAt = runtime.defaultObservedAt.UTC()
	}
	state = harnesspkg.AdjustRuntimeState(harness, state, options.event, attributes, runtime.parentArgs)

	source := options.source
	if source == "" {
		source = "cli"
	}

	confidence := options.confidence
	if confidence == "" {
		confidence = hookConfidence
	}

	report := harnesspkg.WithResumeCommand(registry.Report{
		Harness:       harness,
		State:         state,
		SessionID:     options.sessionID,
		SessionPath:   options.sessionPath,
		ResumeCommand: options.resumeCommand,
		CWD:           options.cwd,
		ProjectRoot:   options.projectRoot,
		PID:           options.pid,
		PPID:          options.ppid,
		TTY:           options.tty,
		Tmux:          runtime.tmux,
		Source:        source,
		Confidence:    confidence,
		Event:         options.event,
		Attributes:    attributes,
		RawPayload:    rawPayload,
		ObservedAt:    observedAt,
	})

	return preparedReport{harness: harness, report: report, stdin: stdinData}, nil
}

func (app *application) runQueuedReport(ctx context.Context, stdin io.Reader, options reportOptions) error {
	now := time.Now().UTC()
	parentArgs := parentProcessArgs(ctx)
	prepared, err := app.prepareReport(stdin, options, reportRuntimeContext{
		parentArgs:        parentArgs,
		defaultObservedAt: now,
	})
	if err != nil {
		return err
	}
	if prepared.ignored {
		return app.writeIgnoredReport(prepared.harness, options.quiet)
	}

	storePath := app.store().Path()
	queue := reportqueue.New(storePath)
	tmuxEnv := tmuxctx.Env{TMUX: os.Getenv("TMUX"), TMUXPane: os.Getenv("TMUX_PANE")}
	minimalTmux := tmuxctx.ContextFromEnv(tmuxEnv)
	cachedTmux := minimalTmux
	if cached, ok := queue.LookupTmuxContext(minimalTmux, now, 0); ok {
		cachedTmux = cached
	}
	result, err := queue.Enqueue(ctx, reportqueue.Envelope{
		Version:       reportqueue.EnvelopeVersion,
		CreatedAt:     now,
		StorePath:     storePath,
		Kind:          reportqueue.KindReport,
		Report:        reportqueue.ReportFromRegistry(prepared.report),
		RawPayloadSet: len(prepared.report.RawPayload) > 0,
		NoTmux:        options.noTmux,
		Stdin:         prepared.stdin,
		Runtime: reportqueue.RuntimeContext{
			CWD:        defaultCWD(),
			ParentArgs: parentArgs,
			Env: map[string]string{
				"TMUX":      tmuxEnv.TMUX,
				"TMUX_PANE": tmuxEnv.TMUXPane,
				"PWD":       os.Getenv("PWD"),
			},
		},
		CachedTmux: cachedTmux,
	}, reportqueue.EnqueueOptions{Now: func() time.Time { return now }})
	if err != nil {
		fallback := options
		fallback.queue = false

		return app.runReport(ctx, bytes.NewReader(prepared.stdin), fallback)
	}
	app.kickQueueDrainer(ctx, storePath)

	return app.writeQueueResult(result, options.quiet)
}

func (app *application) writeIgnoredReport(harness registry.Harness, quiet bool) error {
	if quiet {
		return nil
	}

	return app.writef("ignored %s report: hook payload does not match harness\n", harness)
}

func (app *application) writeQueueResult(result reportqueue.EnqueueResult, quiet bool) error {
	if app.outputJSON {
		return app.writeJSON(result)
	}
	if quiet {
		return nil
	}

	return app.writef("queued\t%s\n", result.ID)
}

func (app *application) writeReportResult(session registry.Session, quiet bool) error {
	if app.outputJSON {
		return app.writeJSON(session)
	}
	if quiet {
		return nil
	}

	return app.writef("%s\t%s\t%s\n", session.ID, session.Harness, session.State)
}

func parseObservedAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	observedAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing observed-at: %w", err)
	}

	return observedAt, nil
}

func parentProcessArgs(ctx context.Context) []string {
	return processArgs(ctx, os.Getppid())
}

func processArgs(ctx context.Context, pid int) []string {
	if pid <= 0 {
		return nil
	}
	if args := procProcessArgs(pid); len(args) > 0 {
		return args
	}

	return psProcessArgs(ctx, pid)
}

func procProcessArgs(pid int) []string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || len(data) == 0 {
		return nil
	}
	trimmed := strings.TrimRight(string(data), "\x00")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\x00")
}

func psProcessArgs(ctx context.Context, pid int) []string {
	output, err := exec.CommandContext(ctx, "ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}

	return strings.Fields(strings.TrimSpace(string(output)))
}

func normalizeReportHarnessAndState(options reportOptions) (registry.Harness, registry.State, error) {
	if strings.TrimSpace(options.harness) == "" {
		return "", "", missingReportHarnessError(options)
	}
	harness, err := harnesspkg.Normalize(options.harness)
	if err != nil {
		return "", "", fmt.Errorf("normalizing harness: %w", err)
	}

	state, err := registry.NormalizeState(options.state)
	if err != nil {
		return "", "", fmt.Errorf("normalizing state: %w", err)
	}

	return harness, state, nil
}

func reportTmuxContext(ctx context.Context, noTmux bool) registry.TmuxContext {
	if noTmux {
		return registry.TmuxContext{}
	}

	collected, err := tmuxctx.Current(ctx)
	if err != nil {
		return registry.TmuxContext{}
	}

	return collected
}

func requireReportIdentity(state registry.State, options reportOptions) error {
	if state == "" && options.sessionID == "" && options.sessionPath == "" {
		return errMissingReportIdentity
	}

	return nil
}

func missingReportHarnessError(options reportOptions) error {
	if options.state != "" {
		return fmt.Errorf(
			"%w: state %q needs --harness <name>\n\n%s",
			errMissingReportHarness,
			options.state,
			reportQuickHelp(reportExampleStateFirst),
		)
	}

	return fmt.Errorf(
		"%w: choose a harness and state\n\n%s",
		errMissingReportHarness,
		reportQuickHelp(reportExampleHarness),
	)
}

func missingReportIdentityError(harness registry.Harness) error {
	return fmt.Errorf(
		"%w: report for %s needs a state, --session-id, or --session-path\n\n%s",
		errMissingReportIdentity,
		harness,
		reportQuickHelp("agent-sessions report "+string(harness)+" running"),
	)
}

func reportQuickHelp(primaryExample string) string {
	return strings.Join([]string{
		"Try:",
		"  " + primaryExample,
		"  " + reportExampleWithSession,
		"",
		"States: " + reportStatesLabel,
		"Use \"agent-sessions report --help\" for all flags.",
	}, "\n")
}

type listOptions struct {
	harness       string
	state         string
	tmuxSession   string
	activeOnly    bool
	liveOnly      bool
	summary       bool
	watch         bool
	absoluteTime  bool
	absoluteSet   bool
	sortBy        string
	sortSet       bool
	desc          bool
	descSet       bool
	noSnapshot    bool
	noSnapshotSet bool
	watchFormat   string
	formatSet     bool
}

func (app *application) newListCommand() *cobra.Command {
	options := listOptions{}
	cmd := &cobra.Command{
		Use:   listCommandName,
		Short: "List known agent sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			options.absoluteSet = flags.Changed("absolute-time")
			options.sortSet = flags.Changed("sort")
			options.descSet = flags.Changed("desc")
			options.noSnapshotSet = flags.Changed("no-snapshot")
			options.formatSet = flags.Changed("format")

			return app.runList(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.harness, "harness", "", "filter by harness")
	cmd.Flags().StringVar(&options.state, "state", "", "filter by state")
	cmd.Flags().StringVar(&options.tmuxSession, "tmux-session", "", "filter by tmux session id or name")
	cmd.Flags().StringVar(&options.sortBy, "sort", "", "sort by: tmux, updated, state-changed, created, ended, event-at, event, harness, state, cwd, id (default: updated)")
	cmd.Flags().BoolVar(&options.activeOnly, "active", false, "only busy sessions in running or waiting states")
	cmd.Flags().BoolVar(&options.liveOnly, "live", false, "only sessions with a known owner that have not exited")
	cmd.Flags().BoolVar(&options.summary, "summary", false, "summarize agent counts by tmux session")
	cmd.Flags().BoolVar(&options.watch, "watch", false, "watch registry state changes")
	cmd.Flags().BoolVar(&options.absoluteTime, "absolute-time", false, "show full RFC3339 timestamps in text output")
	cmd.Flags().BoolVar(&options.desc, "desc", false, "sort descending")
	cmd.Flags().BoolVar(&options.noSnapshot, "no-snapshot", false, "suppress the startup watch snapshot")
	cmd.Flags().StringVar(&options.watchFormat, "format", "", "watch text output format: table, plain")

	return cmd
}

func (app *application) runList(ctx context.Context, options listOptions) error {
	if err := validateListOptions(options); err != nil {
		return err
	}

	if options.watch {
		filter, err := buildFilter(options)
		if err != nil {
			return err
		}

		return app.runWatch(ctx, watchOptions{
			filter:     filter,
			summary:    options.summary,
			noSnapshot: options.noSnapshot,
			format:     options.watchFormat,
			formatSet:  options.formatSet,
			debounce:   0,
			now:        nil,
			ready:      nil,
		})
	}

	if options.summary {
		return app.runListSummary(ctx, options)
	}

	return app.runListSessions(ctx, options)
}

func validateListOptions(options listOptions) error {
	if !options.watch {
		if options.noSnapshotSet {
			return fmt.Errorf("%w: --no-snapshot requires --watch", errInvalidListFlags)
		}
		if options.formatSet {
			return fmt.Errorf("%w: --format requires --watch", errInvalidListFlags)
		}
	}
	if options.watch || options.summary {
		if options.sortSet {
			return fmt.Errorf("%w: --sort only applies to session list output", errInvalidListFlags)
		}
		if options.descSet {
			return fmt.Errorf("%w: --desc only applies to session list output", errInvalidListFlags)
		}
		if options.absoluteSet {
			return fmt.Errorf("%w: --absolute-time only applies to session list output", errInvalidListFlags)
		}
	}

	return nil
}

func (app *application) runListSessions(ctx context.Context, options listOptions) error {
	filter, err := buildFilter(options)
	if err != nil {
		return err
	}

	sessions, err := app.registryStore().List(ctx, filter)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if err := sortListSessions(sessions, options); err != nil {
		return err
	}

	if app.outputJSON {
		return app.writeJSON(sessions)
	}

	writer := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	now := time.Now().UTC()
	if _, err := fmt.Fprintln(writer, "ID\tHARNESS\tSTATE\tTMUX\tWINDOW\tPANE\tCWD\tUPDATED"); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	for _, session := range sessions {
		if _, err := fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			session.ID,
			session.Harness,
			session.State,
			tmuxSessionLabel(session.Tmux),
			tmuxWindowLabel(session.Tmux),
			session.Tmux.PaneID,
			formatHumanPath(session.CWD),
			formatUpdatedAt(session.UpdatedAt, now, options.absoluteTime),
		); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	return nil
}

func (app *application) runListSummary(ctx context.Context, options listOptions) error {
	filter, err := buildFilter(options)
	if err != nil {
		return err
	}

	summaries, err := app.registryStore().SummaryByTmuxSessionWithOptions(ctx, registry.SummaryOptions{
		Filter: filter,
	})
	if err != nil {
		return fmt.Errorf("summarizing sessions: %w", err)
	}

	if app.outputJSON {
		return app.writeJSON(summaries)
	}

	return app.writeSummaryTable(summaries)
}

func (app *application) writeSummaryTable(summaries []registry.Summary) error {
	writer := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	if _, err := fmt.Fprintln(writer, "TMUX\tACTIVE/TOTAL\tRUNNING\tWAITING\tIDLE\tUNKNOWN\tEXITED"); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	labels := summaryTableLabels(summaries)
	for index, summary := range summaries {
		if _, err := fmt.Fprintf(
			writer,
			"%s\t%d/%d\t%d\t%d\t%d\t%d\t%d\n",
			labels[index],
			summary.Active,
			summary.Total,
			summary.Running,
			summary.Waiting,
			summary.Idle,
			summary.Unknown,
			summary.Exited,
		); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	return nil
}

func summaryTableLabels(summaries []registry.Summary) []string {
	baseCounts := make(map[string]int, len(summaries))
	for _, summary := range summaries {
		baseCounts[summaryLabel(summary)]++
	}

	labels := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		label := summaryLabel(summary)
		if baseCounts[label] > 1 && summary.TmuxSessionID != "" && summary.TmuxSessionName != "" {
			label = fmt.Sprintf("%s (%s)", label, summary.TmuxSessionID)
		}
		labels = append(labels, label)
	}

	return labels
}

func (app *application) newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get one session by registry id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := app.registryStore().Get(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, registry.ErrSessionNotFound) {
					return fmt.Errorf("%w: %s", registry.ErrSessionNotFound, args[0])
				}

				return fmt.Errorf("getting session: %w", err)
			}

			if app.outputJSON {
				return app.writeJSON(session)
			}

			return app.writeJSON(session)
		},
	}
}

type scanOptions struct {
	state               string
	staleOwnerlessAfter time.Duration
}

func (app *application) newScanCommand() *cobra.Command {
	options := scanOptions{
		state:               string(registry.StateUnknown),
		staleOwnerlessAfter: defaultStaleOwnerlessAfter,
	}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan tmux panes for supported harness processes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runScan(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.state, "state", options.state, "state to use for newly observed panes")
	cmd.Flags().DurationVar(&options.staleOwnerlessAfter, "stale-ownerless-after", options.staleOwnerlessAfter, "mark ownerless non-exited sessions as exited after this age; negative disables")

	return cmd
}

func (app *application) runScan(ctx context.Context, options scanOptions) error {
	state, err := registry.NormalizeState(options.state)
	if err != nil {
		return fmt.Errorf("normalizing state: %w", err)
	}

	listPanes := app.listTmuxPanes
	if listPanes == nil {
		listPanes = tmuxctx.ListPanes
	}

	panes, err := listPanes(ctx)
	if err != nil {
		return fmt.Errorf("listing tmux panes: %w", err)
	}
	cacheScannedTmuxPanes(ctx, reportqueue.New(app.store().Path()), panes)

	store := app.registryStore()
	_, err = markMissingTmuxPaneSessionsExited(ctx, store, panes)
	if err != nil {
		return err
	}
	if options.staleOwnerlessAfter >= 0 {
		_, err = markStaleOwnerlessSessionsExited(ctx, store, time.Now().UTC(), options.staleOwnerlessAfter)
		if err != nil {
			return err
		}
	}

	sessions := make([]registry.Session, 0, len(panes))
	for _, pane := range panes {
		harness, ok := harnesspkg.FromCommand(pane.CurrentCommand)
		if !ok {
			continue
		}

		session, err := store.Report(ctx, registry.Report{
			Harness:     harness,
			State:       state,
			CWD:         pane.Tmux.PaneCurrentPath,
			ProjectRoot: findProjectRoot(pane.Tmux.PaneCurrentPath),
			Tmux:        pane.Tmux,
			Source:      "tmux-scan",
			Confidence:  "process",
			Attributes: map[string]string{
				"pane_current_command": pane.CurrentCommand,
			},
		})
		if err != nil {
			return fmt.Errorf("reporting scanned pane: %w", err)
		}
		sessions = append(sessions, session)
	}

	if app.outputJSON {
		return app.writeJSON(sessions)
	}

	return app.writef("reported %d session(s)\n", len(sessions))
}

func cacheScannedTmuxPanes(ctx context.Context, queue reportqueue.Queue, panes []tmuxctx.Pane) {
	now := time.Now().UTC()
	for _, pane := range panes {
		_ = queue.StoreTmuxContext(ctx, pane.Tmux, now)
	}
}

func markMissingTmuxPaneSessionsExited(
	ctx context.Context,
	store registry.Store,
	panes []tmuxctx.Pane,
) ([]registry.Session, error) {
	sessions, err := store.List(ctx, registry.Filter{})
	if err != nil {
		return nil, fmt.Errorf("listing sessions for tmux reconciliation: %w", err)
	}

	livePanes := liveTmuxPaneKeys(panes)
	exited := make([]registry.Session, 0)
	for _, session := range sessions {
		if session.State == registry.StateExited || session.Tmux.PaneID == "" {
			continue
		}
		if _, ok := livePanes[tmuxPaneKey(session.Tmux)]; ok {
			continue
		}

		updated, err := store.Report(ctx, registry.Report{
			Harness:       session.Harness,
			State:         registry.StateExited,
			SessionID:     session.SessionID,
			SessionPath:   session.SessionPath,
			ResumeCommand: session.ResumeCommand,
			CWD:           session.CWD,
			ProjectRoot:   session.ProjectRoot,
			PID:           session.PID,
			PPID:          session.PPID,
			TTY:           session.TTY,
			Tmux:          session.Tmux,
			Source:        "tmux-scan",
			Confidence:    "process",
			Event:         "tmux-pane-missing",
			Attributes: map[string]string{
				"agent_sessions_reconcile": "tmux-pane-missing",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("marking missing tmux pane session exited: %w", err)
		}
		exited = append(exited, updated)
	}

	return exited, nil
}

func markStaleOwnerlessSessionsExited(
	ctx context.Context,
	store registry.Store,
	now time.Time,
	staleAfter time.Duration,
) ([]registry.Session, error) {
	sessions, err := store.List(ctx, registry.Filter{})
	if err != nil {
		return nil, fmt.Errorf("listing sessions for ownerless reconciliation: %w", err)
	}

	exited := make([]registry.Session, 0)
	for _, session := range sessions {
		if session.State == registry.StateExited || registry.HasOwner(session) {
			continue
		}
		if !ownerlessSessionIsStale(session, now, staleAfter) {
			continue
		}

		updated, err := store.Report(ctx, registry.Report{
			Harness:       session.Harness,
			State:         registry.StateExited,
			SessionID:     session.SessionID,
			SessionPath:   session.SessionPath,
			ResumeCommand: session.ResumeCommand,
			CWD:           session.CWD,
			ProjectRoot:   session.ProjectRoot,
			TTY:           session.TTY,
			Source:        "registry-reconcile",
			Confidence:    session.Confidence,
			Event:         "ownerless-stale",
			Attributes: map[string]string{
				"agent_sessions_reconcile": "ownerless-stale",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("marking stale ownerless session exited: %w", err)
		}
		exited = append(exited, updated)
	}

	return exited, nil
}

func ownerlessSessionIsStale(session registry.Session, now time.Time, staleAfter time.Duration) bool {
	if staleAfter < 0 {
		return false
	}
	reference := session.LastSeenAt
	if reference.IsZero() {
		reference = session.UpdatedAt
	}
	if reference.IsZero() {
		reference = session.CreatedAt
	}
	if reference.IsZero() {
		return false
	}

	return !reference.After(now) && now.Sub(reference) >= staleAfter
}

func liveTmuxPaneKeys(panes []tmuxctx.Pane) map[string]struct{} {
	keys := make(map[string]struct{}, len(panes))
	for _, pane := range panes {
		key := tmuxPaneKey(pane.Tmux)
		if key != "" {
			keys[key] = struct{}{}
		}
		fallbackKey := tmuxPaneFallbackKey(pane.Tmux)
		if fallbackKey != "" {
			keys[fallbackKey] = struct{}{}
		}
	}

	return keys
}

func tmuxPaneKey(tmux registry.TmuxContext) string {
	if tmux.PaneID == "" {
		return ""
	}
	if tmux.SessionID != "" && tmux.SessionName != "" {
		return "tmux:" + tmux.SessionID + "\x00" + tmux.SessionName + "\x00" + tmux.PaneID
	}
	if tmux.SessionID != "" {
		return "tmux:" + tmux.SessionID + "\x00" + tmux.PaneID
	}

	return tmuxPaneFallbackKey(tmux)
}

func tmuxPaneFallbackKey(tmux registry.TmuxContext) string {
	if tmux.PaneID == "" {
		return ""
	}

	return "pane:" + tmux.PaneID
}

func (app *application) newGCCommand() *cobra.Command {
	var deleteAfter time.Duration
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Delete old exited session records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			effectiveDeleteAfter := deleteAfter
			if !cmd.Flags().Changed("delete-after") {
				effectiveDeleteAfter = -1
			}
			result, err := app.registryStore().GC(cmd.Context(), effectiveDeleteAfter)
			if err != nil {
				return fmt.Errorf("garbage collecting sessions: %w", err)
			}

			if app.outputJSON {
				return app.writeJSON(result)
			}

			return app.writef("deleted=%d remaining=%d\n", result.Deleted, result.Remaining)
		},
	}
	cmd.Flags().DurationVar(&deleteAfter, "delete-after", 0, "delete exited sessions after this age")

	return cmd
}

func buildFilter(options listOptions) (registry.Filter, error) {
	filter := registry.Filter{
		TmuxSession: options.tmuxSession,
		ActiveOnly:  options.activeOnly,
		LiveOnly:    options.liveOnly,
	}

	if options.harness != "" {
		harness, err := harnesspkg.Normalize(options.harness)
		if err != nil {
			return registry.Filter{}, fmt.Errorf("normalizing harness filter: %w", err)
		}

		filter.Harness = harness
	}

	if options.state != "" {
		state, err := registry.NormalizeState(options.state)
		if err != nil {
			return registry.Filter{}, fmt.Errorf("normalizing state filter: %w", err)
		}

		filter.State = state
	}

	return filter, nil
}

func sortListSessions(sessions []registry.Session, options listOptions) error {
	sortBy := strings.TrimSpace(options.sortBy)
	if sortBy == "" {
		sortBy = listSortUpdated
	}

	less, err := listSortLess(sortBy)
	if err != nil {
		return err
	}

	sort.SliceStable(sessions, func(i int, j int) bool {
		cmp := less(sessions[i], sessions[j])
		if options.desc {
			return cmp > 0
		}

		return cmp < 0
	})

	return nil
}

type sessionCompareFunc func(registry.Session, registry.Session) int

var listSortComparers = map[string]sessionCompareFunc{
	"tmux":          compareSessionTmux,
	listSortUpdated: compareSessionUpdated,
	"state-changed": compareSessionStateChanged,
	"created":       compareSessionCreated,
	"ended":         compareSessionEnded,
	"event-at":      compareSessionEventAt,
	"event":         compareSessionEvent,
	"harness":       compareSessionHarness,
	"state":         compareSessionState,
	"cwd":           compareSessionCWD,
	"id":            compareSessionID,
}

func listSortLess(sortBy string) (sessionCompareFunc, error) {
	comparer, ok := listSortComparers[normalizeListSort(sortBy)]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errInvalidListSort, sortBy)
	}

	return comparer, nil
}

func normalizeListSort(sortBy string) string {
	normalized := strings.ToLower(strings.TrimSpace(sortBy))
	normalized = strings.ReplaceAll(normalized, "_", "-")

	switch normalized {
	case "":
		return "tmux"
	case "time", "updated-at", listSortUpdated:
		return listSortUpdated
	case "state-changed-at", "state-since", "state-changed":
		return "state-changed"
	case "created-at", "created":
		return "created"
	case "ended-at", "ended":
		return "ended"
	case "last-event-at", "event-at":
		return "event-at"
	case "last-event", "event":
		return "event"
	default:
		return normalized
	}
}

func compareSessionTmux(left registry.Session, right registry.Session) int {
	if cmp := strings.Compare(left.Tmux.SessionName, right.Tmux.SessionName); cmp != 0 {
		return cmp
	}
	if cmp := compareNumericStrings(left.Tmux.WindowIndex, right.Tmux.WindowIndex); cmp != 0 {
		return cmp
	}
	if cmp := compareNumericStrings(left.Tmux.PaneIndex, right.Tmux.PaneIndex); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(string(left.Harness), string(right.Harness)); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.ID, right.ID); cmp != 0 {
		return cmp
	}

	return compareTime(left.UpdatedAt, right.UpdatedAt)
}

func compareNumericStrings(left string, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr == nil && rightErr == nil {
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		default:
			return 0
		}
	}

	return strings.Compare(left, right)
}

func compareSessionUpdated(left registry.Session, right registry.Session) int {
	return compareTime(left.UpdatedAt, right.UpdatedAt)
}

func compareSessionStateChanged(left registry.Session, right registry.Session) int {
	return compareTime(left.StateChangedAt, right.StateChangedAt)
}

func compareSessionCreated(left registry.Session, right registry.Session) int {
	return compareTime(left.CreatedAt, right.CreatedAt)
}

func compareSessionEnded(left registry.Session, right registry.Session) int {
	return compareTime(left.EndedAt, right.EndedAt)
}

func compareSessionEventAt(left registry.Session, right registry.Session) int {
	return compareTime(left.LastEventAt, right.LastEventAt)
}

func compareSessionEvent(left registry.Session, right registry.Session) int {
	return strings.Compare(left.LastEvent, right.LastEvent)
}

func compareSessionHarness(left registry.Session, right registry.Session) int {
	return strings.Compare(string(left.Harness), string(right.Harness))
}

func compareSessionState(left registry.Session, right registry.Session) int {
	return strings.Compare(string(left.State), string(right.State))
}

func compareSessionCWD(left registry.Session, right registry.Session) int {
	return strings.Compare(left.CWD, right.CWD)
}

func compareSessionID(left registry.Session, right registry.Session) int {
	return strings.Compare(left.ID, right.ID)
}

func compareTime(left time.Time, right time.Time) int {
	switch {
	case left.Equal(right):
		return 0
	case left.IsZero():
		return -1
	case right.IsZero():
		return 1
	case left.Before(right):
		return -1
	default:
		return 1
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}

	return ""
}

func firstEnvInt(names ...string) int {
	value := firstEnv(names...)
	if value == "" {
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}

	return parsed
}

func defaultCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	return cwd
}

func findProjectRoot(start string) string {
	if start == "" {
		return ""
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return start
	}

	for {
		if exists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func parseAttributes(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}

	attributes := make(map[string]string, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("%w: must be key=value: %q", errInvalidAttribute, value)
		}

		attributes[strings.TrimSpace(key)] = item
	}

	return attributes, nil
}

func readStdinPayloadData(
	stdin io.Reader,
	storeRaw bool,
	defaultsOnly bool,
) (json.RawMessage, json.RawMessage, []byte, error) {
	if !storeRaw && !defaultsOnly {
		return nil, nil, nil, nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading stdin payload: %w", err)
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil, nil, nil
	}

	var payload json.RawMessage
	if json.Valid(data) {
		payload = json.RawMessage(data)
	} else {
		wrapped, err := json.Marshal(string(data))
		if err != nil {
			return nil, nil, data, fmt.Errorf("encoding stdin payload: %w", err)
		}
		payload = json.RawMessage(wrapped)
	}

	if storeRaw {
		return payload, payload, data, nil
	}

	return nil, payload, data, nil
}

func applyPayloadDefaults(
	options *reportOptions,
	attributes map[string]string,
	defaults harnesspkg.PayloadDefaults,
) {
	applyStringDefault(&options.sessionID, defaults.SessionID)
	applyStringDefault(&options.sessionPath, defaults.SessionPath)
	applyStringDefault(&options.event, defaults.Event)
	applyProjectRootDefault(options, defaults.ProjectRoot)
	applyCWDDefault(options, defaults.CWD)
	maps.Copy(attributes, defaults.Attributes)
}

func applyReportRuntimeDefaults(options *reportOptions) {
	if options.cwd == "" && options.cwdAuto {
		options.cwd = defaultCWD()
	}
	if options.projectRoot == "" && options.projectRootAuto {
		options.projectRoot = findProjectRoot(options.cwd)
	}
}

func applyStringDefault(target *string, value string) {
	if *target != "" || value == "" {
		return
	}

	*target = value
}

func applyCWDDefault(options *reportOptions, cwd string) {
	if cwd == "" || (options.cwd != "" && !options.cwdAuto) {
		return
	}

	options.cwd = cwd
	if options.projectRoot != "" {
		return
	}

	options.projectRoot = findProjectRoot(cwd)
	options.projectRootAuto = true
}

func applyProjectRootDefault(options *reportOptions, projectRoot string) {
	if projectRoot == "" || (options.projectRoot != "" && !options.projectRootAuto) {
		return
	}

	options.projectRoot = projectRoot
	options.projectRootAuto = true
}

func tmuxSessionLabel(ctx registry.TmuxContext) string {
	if ctx.SessionName != "" {
		return ctx.SessionName
	}

	if ctx.SessionID != "" {
		return ctx.SessionID
	}

	return "-"
}

func tmuxWindowLabel(ctx registry.TmuxContext) string {
	switch {
	case ctx.WindowIndex != "" && ctx.WindowName != "":
		return ctx.WindowIndex + ":" + ctx.WindowName
	case ctx.WindowName != "":
		return ctx.WindowName
	case ctx.WindowIndex != "":
		return ctx.WindowIndex
	default:
		return "-"
	}
}

func summaryLabel(summary registry.Summary) string {
	if summary.TmuxSessionName != "" {
		return summary.TmuxSessionName
	}
	if summary.TmuxSessionID != "" {
		return summary.TmuxSessionID
	}

	return "unknown"
}

func formatUpdatedAt(updatedAt time.Time, now time.Time, absolute bool) string {
	if updatedAt.IsZero() {
		return "-"
	}
	if absolute {
		return updatedAt.Format(time.RFC3339)
	}

	elapsed := now.Sub(updatedAt)
	if elapsed < time.Second {
		return "just now"
	}

	return formatElapsed(elapsed)
}

func formatHumanPath(path string) string {
	if path == "" {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}

	relative, err := filepath.Rel(home, path)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	if relative == "." {
		return "~"
	}

	return filepath.Join("~", relative)
}

func formatElapsed(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed/time.Second))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < dayDuration():
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	case elapsed < weekDuration():
		return fmt.Sprintf("%dd ago", int(elapsed/dayDuration()))
	case elapsed < yearDuration():
		return fmt.Sprintf("%dw ago", int(elapsed/weekDuration()))
	default:
		return fmt.Sprintf("%dy ago", int(elapsed/yearDuration()))
	}
}

func dayDuration() time.Duration {
	return hoursPerDay * time.Hour
}

func weekDuration() time.Duration {
	return daysPerWeek * dayDuration()
}

func yearDuration() time.Duration {
	return daysPerYear * dayDuration()
}
