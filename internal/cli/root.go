package cli

import (
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

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

var (
	version                      = "dev"
	commit                       = "none"
	date                         = "unknown"
	errInvalidAttribute          = errors.New("invalid attribute")
	errInvalidListSort           = errors.New("invalid list sort")
	errUnexpectedReportArg       = errors.New("unexpected report argument")
	errMissingReportHarness      = errors.New("missing harness")
	errMissingReportIdentity     = errors.New("missing report identity or transition")
	errDoctorFailed              = errors.New("doctor found errors")
	errInvalidObserveInterval    = errors.New("interval must be positive")
	errInvalidObserveGracePeriod = errors.New("grace period must be nonnegative")
	errGonePresenceActivity      = errors.New("gone presence cannot include activity")
	errProcessEvidenceIdentity   = errors.New("process evidence requires pid and start identity")
	errProcessEvidenceActivity   = errors.New("process evidence cannot include activity")
)

const (
	tabPadding         = 2
	reportCommandName  = "report"
	listCommandName    = "list"
	hookCommandName    = "hook"
	agyHookCommandName = "agy-hook"
	listSortUpdated    = "updated"
	hoursPerDay        = 24
)

type application struct {
	storePath  string
	outputJSON bool
	stdout     io.Writer
	stderr     io.Writer
}

func NewRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	app := &application{stdout: stdout, stderr: stderr}
	var showVersion bool
	root := &cobra.Command{Use: "agent-sessions", Short: "Track local coding-agent sessions across harnesses and tmux panes", RunE: func(cmd *cobra.Command, _ []string) error {
		if showVersion {
			return app.writef("agent-sessions %s (commit: %s, built: %s)\n", version, commit, date)
		}
		return cmd.Help()
	}, CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true}}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&app.storePath, "store", "", "registry state file path")
	root.PersistentFlags().BoolVar(&app.outputJSON, "json", false, "emit JSON")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")
	root.AddCommand(app.newReportCommand(), app.newListCommand(), app.newGetCommand(), app.newGCCommand(), app.newManageCommand(), app.newPathCommand(), app.newInstallHooksCommand(), app.newHookCommand(), app.newAgyHookCommand(), app.newDrainQueueCommand(), app.newQueueStatusCommand(), app.newObserveCommand(), app.newServiceCommand(), app.newDoctorCommand())
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
	if err := NewRootCommand(os.Stdout, os.Stderr).Execute(); err != nil {
		os.Exit(1)
	}
}

func (app *application) store() *registry.FileStore    { return registry.NewFileStore(app.storePath) }
func (app *application) registryStore() registry.Store { return app.store() }
func (app *application) writeJSON(value any) error {
	e := json.NewEncoder(app.stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(value); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}

func (app *application) writef(format string, args ...any) error {
	if _, err := fmt.Fprintf(app.stdout, format, args...); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func (app *application) writeln(args ...any) error {
	if _, err := fmt.Fprintln(app.stdout, args...); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func (app *application) warnf(format string, args ...any) {
	if app.stderr != nil {
		_, _ = fmt.Fprintf(app.stderr, format, args...)
	}
}

func (app *application) newPathCommand() *cobra.Command {
	return &cobra.Command{Use: "path", Short: "Print the registry state file path", RunE: func(_ *cobra.Command, _ []string) error {
		if app.outputJSON {
			return app.writeJSON(map[string]string{"path": app.store().Path()})
		}
		return app.writeln(app.store().Path())
	}}
}

type reportOptions struct {
	harness         string
	presence        string
	activity        string
	sessionID       string
	sessionPath     string
	cwd             string
	cwdAuto         bool
	projectRoot     string
	projectRootAuto bool
	pid             int
	ppid            int
	processGroupID  int
	startIdentity   string
	executable      string
	tty             string
	event           string
	observedAt      string
	attributes      []string
	rawStdin        bool
	rawDefaultsOnly bool
	noTmux          bool
	queue           bool
	quiet           bool
	resumeCommand   []string
	evidence        string
}
type preparedReport struct {
	harness     registry.Harness
	observation registry.Observation
	stdin       []byte
	ignored     bool
}
type reportRuntimeContext struct {
	tmux              registry.TmuxContext
	defaultObservedAt time.Time
}

func (app *application) newReportCommand() *cobra.Command {
	options := defaultReportOptionsFromEnv()
	cmd := &cobra.Command{Use: "report [harness]", Short: "Record a harness observation", SilenceUsage: true, Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			if options.harness != "" {
				return fmt.Errorf("%w: harness already set", errUnexpectedReportArg)
			}
			options.harness = args[0]
		}
		if cmd.Flags().Changed("cwd") {
			options.cwdAuto = false
		}
		if cmd.Flags().Changed("project-root") {
			options.projectRootAuto = false
		}
		return app.runReport(cmd.Context(), cmd.InOrStdin(), options)
	}}
	f := cmd.Flags()
	f.StringVar(&options.presence, "presence", options.presence, "presence: live, gone, unknown")
	f.StringVar(&options.activity, "activity", options.activity, "activity: running, waiting, idle, unknown")
	f.StringVar(&options.sessionID, "session-id", options.sessionID, "harness session id")
	f.StringVar(&options.sessionPath, "session-path", options.sessionPath, "harness session file path")
	f.StringVar(&options.cwd, "cwd", options.cwd, "agent current working directory")
	f.StringVar(&options.projectRoot, "project-root", options.projectRoot, "project root")
	f.IntVar(&options.pid, "pid", options.pid, "agent process id")
	f.IntVar(&options.ppid, "ppid", options.ppid, "agent parent process id")
	f.IntVar(&options.processGroupID, "process-group-id", options.processGroupID, "agent process group id")
	f.StringVar(&options.startIdentity, "start-identity", options.startIdentity, "process start identity")
	f.StringVar(&options.executable, "executable", options.executable, "resolved executable path")
	f.StringVar(&options.tty, "tty", options.tty, "agent tty")
	f.StringVar(&options.event, "event", options.event, "native harness event name")
	f.StringVar(&options.observedAt, "observed-at", options.observedAt, "RFC3339 timestamp")
	f.StringArrayVar(&options.attributes, "attribute", nil, "extra key=value attribute")
	f.StringArrayVar(&options.resumeCommand, "resume-command", nil, "resume command argv item, repeatable")
	f.StringVar(&options.evidence, "evidence", options.evidence, "evidence kind (managed shims)")
	f.BoolVar(&options.rawStdin, "raw-stdin", false, "store stdin as raw hook payload")
	f.BoolVar(&options.rawDefaultsOnly, "raw-stdin-defaults-only", false, "read stdin for defaults without storing raw payload")
	f.BoolVar(&options.noTmux, "no-tmux", false, "do not collect tmux context")
	f.BoolVar(&options.queue, "queue", false, "durably queue observation")
	_ = f.MarkHidden("queue")
	_ = f.MarkHidden("evidence")
	f.BoolVar(&options.quiet, "quiet", false, "suppress output")
	return cmd
}

func defaultReportOptionsFromEnv() reportOptions {
	return reportOptions{harness: firstEnv("AGENT_SESSIONS_HARNESS", "AGENT_HARNESS"), sessionID: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionID)...), sessionPath: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionPath)...), cwdAuto: true, projectRoot: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvProjectRoot)...), pid: firstEnvInt(harnesspkg.EnvNames(harnesspkg.EnvPID)...), ppid: firstEnvInt("AGENT_SESSIONS_PPID", "AGENT_PPID"), tty: firstEnv("AGENT_SESSIONS_TTY", "TTY"), event: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvEvent)...)}
}

func parseObservedAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing observed-at: %w", err)
	}
	return t, nil
}

func (app *application) runReport(ctx context.Context, stdin io.Reader, options reportOptions) error {
	if options.queue {
		return app.runQueuedReport(ctx, stdin, options)
	}
	prepared, err := app.prepareReport(stdin, options, reportRuntimeContext{tmux: reportTmuxContext(ctx, options.noTmux)})
	if err != nil {
		return err
	}
	if prepared.ignored {
		if options.quiet {
			return nil
		}
		return app.writef("ignored %s report: hook payload does not match harness\n", prepared.harness)
	}
	session, err := app.registryStore().Observe(ctx, prepared.observation)
	if err != nil {
		return fmt.Errorf("recording observation: %w", err)
	}
	return app.writeReportResult(session, options.quiet)
}

//nolint:gocognit,cyclop // report preparation deliberately validates independent evidence dimensions in order
func (app *application) prepareReport(stdin io.Reader, options reportOptions, runtime reportRuntimeContext) (preparedReport, error) {
	if strings.TrimSpace(options.harness) == "" {
		return preparedReport{}, errMissingReportHarness
	}
	harness, err := harnesspkg.Normalize(options.harness)
	if err != nil {
		return preparedReport{}, fmt.Errorf("normalizing harness: %w", err)
	}
	attrs, err := parseAttributes(options.attributes)
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
	applyPayloadDefaults(&options, attrs, harnesspkg.DefaultsFromPayload(harness, defaultsPayload))
	applyReportRuntimeDefaults(&options)
	presence, err := registry.NormalizePresence(options.presence)
	if err != nil {
		return preparedReport{}, fmt.Errorf("normalize presence: %w", err)
	}
	activity, err := registry.NormalizeActivity(options.activity)
	if err != nil {
		return preparedReport{}, fmt.Errorf("normalize activity: %w", err)
	}
	if presence == registry.PresenceGone && activity != "" {
		return preparedReport{}, errGonePresenceActivity
	}
	observedAt, err := parseObservedAt(options.observedAt)
	if err != nil {
		return preparedReport{}, err
	}
	if observedAt.IsZero() {
		observedAt = runtime.defaultObservedAt
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if presence == "" && activity == "" && options.event == "" && options.sessionID == "" && options.sessionPath == "" {
		return preparedReport{}, errMissingReportIdentity
	}
	identity := registry.ObservationIdentity{SessionID: options.sessionID, SessionPath: options.sessionPath}
	observation := registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: harness, Identity: identity, NativeEvent: options.event, Attributes: attrs, RawPayload: rawPayload, ObservedAt: observedAt}
	if strings.EqualFold(options.evidence, "process") {
		if options.pid <= 0 || options.startIdentity == "" {
			return preparedReport{}, errProcessEvidenceIdentity
		}
		if activity != "" {
			return preparedReport{}, errProcessEvidenceActivity
		}
		present := presence != registry.PresenceGone
		observation.Source, observation.Evidence = registry.ObservationSourceProcess, registry.ObservationEvidenceProcessPresence
		observation.NativeEvent = ""
		observation.ProcessPresent = &present
		observation.Process = &registry.ProcessIdentity{PID: options.pid, PPID: options.ppid, ProcessGroupID: options.processGroupID, StartIdentity: options.startIdentity, Executable: options.executable, CWD: options.cwd, TTY: options.tty}
	}
	if observation.Source == registry.ObservationSourceNative && presence != "" {
		observation.Presence = &presence
	}
	if activity != "" {
		observation.Activity = &activity
	}
	if observation.Source == registry.ObservationSourceNative && options.event == "" && (presence != "" || activity != "") {
		observation.NativeEvent = "cli"
	}
	if options.cwd != "" || options.projectRoot != "" || len(options.resumeCommand) > 0 {
		observation.Catalog = &registry.CatalogMetadata{ResumeCommand: append([]string(nil), options.resumeCommand...), CWD: options.cwd, ProjectRoot: options.projectRoot}
	}
	return preparedReport{harness: harness, observation: observation, stdin: stdinData}, nil
}

func appReportActivity(session registry.Session) string {
	if session.Activity == nil {
		return "null"
	}
	return string(*session.Activity)
}

func (app *application) writeReportResult(session registry.Session, quiet bool) error {
	if app.outputJSON {
		return app.writeJSON(session)
	}
	if quiet {
		return nil
	}
	return app.writef("%s\t%s\t%s\t%s\n", session.ID, session.Harness, session.Presence, appReportActivity(session))
}

func reportTmuxContext(ctx context.Context, noTmux bool) registry.TmuxContext {
	if noTmux {
		return registry.TmuxContext{}
	}
	t, err := tmuxctx.Current(ctx)
	if err != nil {
		return registry.TmuxContext{}
	}
	return t
}

func parentProcessArgs(ctx context.Context) []string { return processArgs(ctx, os.Getppid()) }
func processArgs(ctx context.Context, pid int) []string {
	if pid <= 0 {
		return nil
	}
	if a := procProcessArgs(pid); len(a) > 0 {
		return a
	}
	return psProcessArgs(ctx, pid)
}

func procProcessArgs(pid int) []string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
}

func psProcessArgs(ctx context.Context, pid int) []string {
	out, err := exec.CommandContext(ctx, "ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

// list.
type listOptions struct {
	harness, presence, activity, tmuxSession, sortBy, watchFormat                                           string
	summary, watch, absoluteTime, absoluteSet, sortSet, desc, descSet, noSnapshot, noSnapshotSet, formatSet bool
}

func (app *application) newListCommand() *cobra.Command {
	o := listOptions{}
	cmd := &cobra.Command{Use: "list", Short: "List known agent sessions", RunE: func(c *cobra.Command, _ []string) error {
		f := c.Flags()
		o.absoluteSet = f.Changed("absolute-time")
		o.sortSet = f.Changed("sort")
		o.descSet = f.Changed("desc")
		o.noSnapshotSet = f.Changed("no-snapshot")
		o.formatSet = f.Changed("format")
		return app.runList(c.Context(), o)
	}}
	f := cmd.Flags()
	f.StringVar(&o.harness, "harness", "", "filter by harness")
	f.StringVar(&o.presence, "presence", "", "filter by presence")
	f.StringVar(&o.activity, "activity", "", "filter by activity")
	f.StringVar(&o.tmuxSession, "tmux-session", "", "filter by tmux session")
	f.StringVar(&o.sortBy, "sort", "", "sort by: tmux, updated, presence-changed, activity-changed, created, harness, presence, activity, cwd, id")
	f.BoolVar(&o.summary, "summary", false, "summarize agent counts by tmux session")
	f.BoolVar(&o.watch, "watch", false, "watch registry changes")
	f.BoolVar(&o.absoluteTime, "absolute-time", false, "show full timestamps")
	f.BoolVar(&o.desc, "desc", false, "sort descending")
	f.BoolVar(&o.noSnapshot, "no-snapshot", false, "suppress startup snapshot")
	f.StringVar(&o.watchFormat, "format", "", "watch output format: table, plain")
	return cmd
}

func (app *application) runList(ctx context.Context, o listOptions) error {
	if o.watch {
		p, e := buildFilter(o)
		if e != nil {
			return e
		}
		return app.runWatch(ctx, watchOptions{filter: p, summary: o.summary, noSnapshot: o.noSnapshot, format: o.watchFormat, formatSet: o.formatSet})
	}
	if o.summary {
		return app.runListSummary(ctx, o)
	}
	return app.runListSessions(ctx, o)
}

func buildFilter(o listOptions) (registry.Filter, error) {
	f := registry.Filter{TmuxSession: o.tmuxSession}
	if o.harness != "" {
		h, e := harnesspkg.Normalize(o.harness)
		if e != nil {
			return f, fmt.Errorf("normalize harness: %w", e)
		}
		f.Harness = h
	}
	if o.presence != "" {
		p, e := registry.NormalizePresence(o.presence)
		if e != nil {
			return f, fmt.Errorf("normalize presence: %w", e)
		}
		f.Presence = p
	}
	if o.activity != "" {
		a, e := registry.NormalizeActivity(o.activity)
		if e != nil {
			return f, fmt.Errorf("normalize activity: %w", e)
		}
		f.Activity = a
	}
	return f, nil
}

func (app *application) runListSessions(ctx context.Context, o listOptions) error {
	f, e := buildFilter(o)
	if e != nil {
		return e
	}
	ss, e := app.registryStore().List(ctx, f)
	if e != nil {
		return fmt.Errorf("listing sessions: %w", e)
	}
	if e = sortListSessions(ss, o); e != nil {
		return e
	}
	if app.outputJSON {
		return app.writeJSON(ss)
	}
	w := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	if _, e = fmt.Fprintln(w, "ID\tHARNESS\tPRESENCE\tACTIVITY\tTMUX\tWINDOW\tPANE\tCWD\tUPDATED"); e != nil {
		return fmt.Errorf("write list header: %w", e)
	}
	now := time.Now().UTC()
	for _, s := range ss {
		activity := appReportActivity(s)
		if s.Presence == registry.PresenceGone {
			activity = "-"
		}
		_, e = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Harness, s.Presence, activity, tmuxSessionLabel(s.Tmux), tmuxWindowLabel(s.Tmux), s.Tmux.PaneID, formatHumanPath(s.CWD), formatUpdatedAt(s.UpdatedAt, now, o.absoluteTime))
		if e != nil {
			return fmt.Errorf("write list row: %w", e)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush list output: %w", err)
	}
	return nil
}

func (app *application) runListSummary(ctx context.Context, o listOptions) error {
	f, e := buildFilter(o)
	if e != nil {
		return e
	}
	s, e := app.registryStore().SummaryByTmuxSessionWithOptions(ctx, registry.SummaryOptions{Filter: f})
	if e != nil {
		return fmt.Errorf("summarize sessions: %w", e)
	}
	if app.outputJSON {
		return app.writeJSON(s)
	}
	return app.writeSummaryTable(s)
}

func (app *application) writeSummaryTable(ss []registry.Summary) error {
	w := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	if _, e := fmt.Fprintln(w, "TMUX\tTOTAL\tLIVE\tGONE\tPRESENCE_UNKNOWN\tRUNNING\tWAITING\tIDLE\tACTIVITY_UNKNOWN"); e != nil {
		return fmt.Errorf("write summary header: %w", e)
	}
	labels := summaryTableLabels(ss)
	for i, s := range ss {
		if _, e := fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", labels[i], s.Total, s.Live, s.Gone, s.PresenceUnknown, s.Running, s.Waiting, s.Idle, s.ActivityUnknown); e != nil {
			return fmt.Errorf("write summary row: %w", e)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush summary output: %w", err)
	}
	return nil
}

//nolint:gocritic // label precedence is intentionally explicit for stable output
func summaryTableLabels(ss []registry.Summary) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s.TmuxSessionName != "" {
			out = append(out, s.TmuxSessionName)
		} else if s.TmuxSessionID != "" {
			out = append(out, s.TmuxSessionID)
		} else {
			out = append(out, "unknown")
		}
	}
	return out
}

func (app *application) newGetCommand() *cobra.Command {
	return &cobra.Command{Use: "get <id>", Short: "Get one session by registry id", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		s, e := app.registryStore().Get(c.Context(), a[0])
		if e != nil {
			return fmt.Errorf("get session: %w", e)
		}
		return app.writeJSON(s)
	}}
}

func (app *application) newGCCommand() *cobra.Command {
	var age time.Duration
	c := &cobra.Command{Use: "gc", Short: "Delete old gone session records", RunE: func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("delete-after") {
			age = -1
		}
		r, e := app.registryStore().GC(cmd.Context(), age)
		if e != nil {
			return fmt.Errorf("gc sessions: %w", e)
		}
		if app.outputJSON {
			return app.writeJSON(r)
		}
		return app.writef("deleted=%d remaining=%d\n", r.Deleted, r.Remaining)
	}}
	c.Flags().DurationVar(&age, "delete-after", 0, "delete gone sessions after this age")
	return c
}

type sessionCompareFunc func(registry.Session, registry.Session) int

func sortListSessions(ss []registry.Session, o listOptions) error {
	key := normalizeListSort(o.sortBy)
	cmp, e := listSortLess(key)
	if e != nil {
		return e
	}
	sort.SliceStable(ss, func(i, j int) bool {
		v := cmp(ss[i], ss[j])
		if o.desc {
			return v > 0
		}
		return v < 0
	})
	return nil
}

func listSortLess(k string) (sessionCompareFunc, error) {
	if c, ok := map[string]sessionCompareFunc{"tmux": compareSessionTmux, "updated": compareSessionUpdated, "presence-changed": func(a, b registry.Session) int { return compareTime(a.PresenceChangedAt, b.PresenceChangedAt) }, "activity-changed": func(a, b registry.Session) int { return compareTime(a.ActivityChangedAt, b.ActivityChangedAt) }, "created": compareSessionCreated, "harness": func(a, b registry.Session) int { return strings.Compare(string(a.Harness), string(b.Harness)) }, "presence": func(a, b registry.Session) int { return strings.Compare(string(a.Presence), string(b.Presence)) }, "activity": func(a, b registry.Session) int { return strings.Compare(appReportActivity(a), appReportActivity(b)) }, "cwd": func(a, b registry.Session) int { return strings.Compare(a.CWD, b.CWD) }, "id": compareSessionID}[k]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("%w: %q", errInvalidListSort, k)
}

func normalizeListSort(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"))
	switch s {
	case "", "time", "updated-at":
		return "updated"
	case "presence-changed-at", "presence-since":
		return "presence-changed"
	case "activity-changed-at", "activity-since":
		return "activity-changed"
	}
	return s
}

func compareSessionTmux(a, b registry.Session) int {
	if c := strings.Compare(a.Tmux.SessionName, b.Tmux.SessionName); c != 0 {
		return c
	}
	if c := strings.Compare(a.Tmux.WindowIndex, b.Tmux.WindowIndex); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

func compareSessionUpdated(a, b registry.Session) int { return compareTime(a.UpdatedAt, b.UpdatedAt) }

func compareSessionCreated(a, b registry.Session) int { return compareTime(a.CreatedAt, b.CreatedAt) }
func compareSessionID(a, b registry.Session) int      { return strings.Compare(a.ID, b.ID) }
func compareTime(a, b time.Time) int {
	if a.Equal(b) {
		return 0
	}
	if a.IsZero() {
		return -1
	}
	if b.IsZero() || a.After(b) {
		return 1
	}
	return -1
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}
func firstEnvInt(names ...string) int { v := firstEnv(names...); n, _ := strconv.Atoi(v); return n }
func defaultCWD() string              { v, _ := os.Getwd(); return v }
func findProjectRoot(start string) string {
	if start == "" {
		return ""
	}
	d, _ := filepath.Abs(start)
	for {
		if exists(filepath.Join(d, ".git")) {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}
func exists(path string) bool { _, e := os.Stat(path); return e == nil }
func parseAttributes(values []string) (map[string]string, error) {
	a := map[string]string{}
	for _, v := range values {
		k, x, ok := strings.Cut(v, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("%w: must be key=value: %q", errInvalidAttribute, v)
		}
		a[strings.TrimSpace(k)] = x
	}
	return a, nil
}

func readStdinPayloadData(stdin io.Reader, storeRaw, defaultsOnly bool) (json.RawMessage, json.RawMessage, []byte, error) {
	if !storeRaw && !defaultsOnly {
		return nil, nil, nil, nil
	}
	d, err := io.ReadAll(stdin)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read stdin payload: %w", err)
	}
	d = []byte(strings.TrimSpace(string(d)))
	if len(d) == 0 {
		return nil, nil, nil, nil
	}
	var p json.RawMessage
	if json.Valid(d) {
		p = json.RawMessage(d)
	} else {
		encoded, err := json.Marshal(string(d))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("encode stdin payload: %w", err)
		}
		p = encoded
	}
	if storeRaw {
		return p, p, d, nil
	}
	return nil, p, d, nil
}

func applyPayloadDefaults(o *reportOptions, a map[string]string, d harnesspkg.PayloadDefaults) {
	applyStringDefault(&o.sessionID, d.SessionID)
	applyStringDefault(&o.sessionPath, d.SessionPath)
	applyStringDefault(&o.event, d.Event)
	applyCWDDefault(o, d.CWD)
	applyProjectRootDefault(o, d.ProjectRoot)
	maps.Copy(a, d.Attributes)
}

func applyReportRuntimeDefaults(o *reportOptions) {
	if o.cwd == "" && o.cwdAuto {
		o.cwd = defaultCWD()
	}
	if o.projectRoot == "" && o.projectRootAuto {
		o.projectRoot = findProjectRoot(o.cwd)
	}
}

func applyStringDefault(p *string, v string) {
	if *p == "" {
		*p = v
	}
}

func applyCWDDefault(o *reportOptions, v string) {
	if v != "" && o.cwdAuto && o.cwd == "" {
		o.cwd = v
		o.projectRoot = findProjectRoot(v)
	}
}

func applyProjectRootDefault(o *reportOptions, v string) {
	if v != "" && o.projectRootAuto && o.projectRoot == "" {
		o.projectRoot = v
	}
}

func tmuxSessionLabel(c registry.TmuxContext) string {
	if c.SessionName != "" {
		return c.SessionName
	}
	if c.SessionID != "" {
		return c.SessionID
	}
	return "-"
}

func tmuxWindowLabel(c registry.TmuxContext) string {
	if c.WindowIndex != "" && c.WindowName != "" {
		return c.WindowIndex + ":" + c.WindowName
	}
	if c.WindowName != "" {
		return c.WindowName
	}
	if c.WindowIndex != "" {
		return c.WindowIndex
	}
	return "-"
}

func formatUpdatedAt(t, now time.Time, absolute bool) string {
	if t.IsZero() {
		return "-"
	}
	if absolute {
		return t.Format(time.RFC3339)
	}
	d := now.Sub(t)
	if d < time.Second {
		return "just now"
	}
	return formatElapsed(d)
}

func formatHumanPath(p string) string {
	if p == "" {
		return ""
	}
	h, e := os.UserHomeDir()
	if e != nil {
		return p
	}
	r, e := filepath.Rel(h, p)
	if e != nil || filepath.IsAbs(r) || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return p
	}
	if r == "." {
		return "~"
	}
	return filepath.Join("~", r)
}

func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < hoursPerDay*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 7*hoursPerDay*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(hoursPerDay*time.Hour)))
	case d < 365*hoursPerDay*time.Hour:
		return fmt.Sprintf("%dw ago", int(d/(7*hoursPerDay*time.Hour)))
	default:
		return fmt.Sprintf("%dy ago", int(d/(365*hoursPerDay*time.Hour)))
	}
}
