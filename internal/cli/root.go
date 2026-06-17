package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	version = "dev"
	commit  = "none"
	date    = "unknown"

	errInvalidAttribute = errors.New("invalid attribute")
	errInvalidListSort  = errors.New("invalid list sort")
)

const tabPadding = 2

const (
	secondsPerMinute = 60
	minutesPerHour   = 60
	hoursPerDay      = 24
	daysPerWeek      = 7
	daysPerYear      = 365
)

type application struct {
	storePath  string
	outputJSON bool
	stdout     io.Writer
}

func NewRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	app := &application{
		stdout: stdout,
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
		app.newSummaryCommand(),
		app.newScanCommand(),
		app.newGCCommand(),
		app.newPathCommand(),
		app.newInstallHooksCommand(),
	)

	return root
}

func Execute() {
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
	encodeErr := encoder.Encode(value)
	if encodeErr != nil {
		return fmt.Errorf("writing JSON: %w", encodeErr)
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
	attributes      []string
	rawStdin        bool
	noTmux          bool
	quiet           bool
	resumeCommand   []string
}

func (app *application) newReportCommand() *cobra.Command {
	options := reportOptions{
		harness:     firstEnv("AGENT_SESSIONS_HARNESS", "AGENT_HARNESS"),
		state:       firstEnv("AGENT_SESSIONS_STATE", "AGENT_STATE"),
		sessionID:   firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionID)...),
		sessionPath: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvSessionPath)...),
		cwd:         defaultCWD(),
		cwdAuto:     true,
		projectRoot: firstEnv(harnesspkg.EnvNames(harnesspkg.EnvProjectRoot)...),
		pid:         firstEnvInt(harnesspkg.EnvNames(harnesspkg.EnvPID)...),
		ppid:        firstEnvInt("AGENT_SESSIONS_PPID", "AGENT_PPID"),
		tty:         firstEnv("AGENT_SESSIONS_TTY", "TTY"),
		source:      firstEnv("AGENT_SESSIONS_SOURCE"),
		confidence:  firstEnv("AGENT_SESSIONS_CONFIDENCE"),
		event:       firstEnv(harnesspkg.EnvNames(harnesspkg.EnvEvent)...),
	}
	if options.projectRoot == "" {
		options.projectRoot = findProjectRoot(options.cwd)
		options.projectRootAuto = true
	}

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Upsert a session report from a harness hook or wrapper",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && options.state == "" {
				options.state = args[0]
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
	cmd.Flags().StringVar(&options.state, "state", options.state, "state: idle, running, waiting, unknown, exited, stale")
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
	cmd.Flags().StringArrayVar(&options.attributes, "attribute", nil, "extra key=value attribute")
	cmd.Flags().StringArrayVar(&options.resumeCommand, "resume-command", nil, "explicit resume command argv item, repeatable")
	cmd.Flags().BoolVar(&options.rawStdin, "raw-stdin", false, "store stdin as raw hook payload")
	cmd.Flags().BoolVar(&options.noTmux, "no-tmux", false, "do not auto-collect current tmux pane context")
	cmd.Flags().BoolVar(&options.quiet, "quiet", false, "suppress normal report output")

	return cmd
}

func (app *application) runReport(ctx context.Context, stdin io.Reader, options reportOptions) error {
	harness, err := harnesspkg.Normalize(options.harness)
	if err != nil {
		return fmt.Errorf("normalizing harness: %w", err)
	}

	state, err := registry.NormalizeState(options.state)
	if err != nil {
		return fmt.Errorf("normalizing state: %w", err)
	}

	tmux := registry.TmuxContext{}
	if !options.noTmux {
		collected, tmuxErr := tmuxctx.Current(ctx)
		if tmuxErr == nil {
			tmux = collected
		}
	}

	attributes, err := parseAttributes(options.attributes)
	if err != nil {
		return err
	}

	rawPayload, err := readRawPayload(stdin, options.rawStdin)
	if err != nil {
		return err
	}
	applyPayloadDefaults(&options, attributes, harnesspkg.DefaultsFromPayload(harness, rawPayload))

	source := options.source
	if source == "" {
		source = "cli"
	}

	confidence := options.confidence
	if confidence == "" {
		confidence = "hook"
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
		Tmux:          tmux,
		Source:        source,
		Confidence:    confidence,
		Event:         options.event,
		Attributes:    attributes,
		RawPayload:    rawPayload,
	})

	session, err := app.registryStore().Report(ctx, report)
	if err != nil {
		return fmt.Errorf("reporting session: %w", err)
	}

	if app.outputJSON {
		return app.writeJSON(session)
	}
	if options.quiet {
		return nil
	}

	return app.writef("%s\t%s\t%s\n", session.ID, session.Harness, session.State)
}

type listOptions struct {
	harness      string
	state        string
	tmuxSession  string
	activeOnly   bool
	absoluteTime bool
	sortBy       string
	desc         bool
}

func (app *application) newListCommand() *cobra.Command {
	options := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List known agent sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runList(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.harness, "harness", "", "filter by harness")
	cmd.Flags().StringVar(&options.state, "state", "", "filter by state")
	cmd.Flags().StringVar(&options.tmuxSession, "tmux-session", "", "filter by tmux session id or name")
	cmd.Flags().StringVar(&options.sortBy, "sort", "", "sort by: tmux, updated, state-changed, created, ended, event-at, event, harness, state, cwd, id")
	cmd.Flags().BoolVar(&options.activeOnly, "active", false, "only sessions in running or waiting states")
	cmd.Flags().BoolVar(&options.absoluteTime, "absolute-time", false, "show full RFC3339 timestamps in text output")
	cmd.Flags().BoolVar(&options.desc, "desc", false, "sort descending")

	return cmd
}

func (app *application) runList(ctx context.Context, options listOptions) error {
	filter, err := buildFilter(options)
	if err != nil {
		return err
	}

	sessions, err := app.registryStore().List(ctx, filter)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	sortErr := sortListSessions(sessions, options)
	if sortErr != nil {
		return sortErr
	}

	if app.outputJSON {
		return app.writeJSON(sessions)
	}

	writer := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	now := time.Now().UTC()
	_, headerErr := fmt.Fprintln(writer, "ID\tHARNESS\tSTATE\tTMUX\tWINDOW\tPANE\tCWD\tUPDATED")
	if headerErr != nil {
		return fmt.Errorf("writing output: %w", headerErr)
	}

	for _, session := range sessions {
		_, rowErr := fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			session.ID,
			session.Harness,
			session.State,
			tmuxSessionLabel(session.Tmux),
			tmuxWindowLabel(session.Tmux),
			session.Tmux.PaneID,
			session.CWD,
			formatUpdatedAt(session.UpdatedAt, now, options.absoluteTime),
		)
		if rowErr != nil {
			return fmt.Errorf("writing output: %w", rowErr)
		}
	}

	flushErr := writer.Flush()
	if flushErr != nil {
		return fmt.Errorf("flushing output: %w", flushErr)
	}

	return nil
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

func (app *application) newSummaryCommand() *cobra.Command {
	options := listOptions{}
	var staleAfter time.Duration
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Summarize agent counts by tmux session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			filter, err := buildFilter(options)
			if err != nil {
				return err
			}

			summaries, err := app.registryStore().SummaryByTmuxSessionWithOptions(cmd.Context(), registry.SummaryOptions{
				Filter:     filter,
				StaleAfter: staleAfter,
				Now:        time.Time{},
			})
			if err != nil {
				return fmt.Errorf("summarizing sessions: %w", err)
			}

			if app.outputJSON {
				return app.writeJSON(summaries)
			}

			writer := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
			_, headerErr := fmt.Fprintln(writer, "TMUX\tACTIVE/TOTAL\tRUNNING\tWAITING\tIDLE\tUNKNOWN\tSTALE\tEXITED")
			if headerErr != nil {
				return fmt.Errorf("writing output: %w", headerErr)
			}

			for _, summary := range summaries {
				_, rowErr := fmt.Fprintf(
					writer,
					"%s\t%d/%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
					summaryLabel(summary),
					summary.Active,
					summary.Total,
					summary.Running,
					summary.Waiting,
					summary.Idle,
					summary.Unknown,
					summary.Stale,
					summary.Exited,
				)
				if rowErr != nil {
					return fmt.Errorf("writing output: %w", rowErr)
				}
			}

			flushErr := writer.Flush()
			if flushErr != nil {
				return fmt.Errorf("flushing output: %w", flushErr)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&options.harness, "harness", "", "filter by harness")
	cmd.Flags().StringVar(&options.state, "state", "", "filter by state")
	cmd.Flags().StringVar(&options.tmuxSession, "tmux-session", "", "filter by tmux session id or name")
	cmd.Flags().BoolVar(&options.activeOnly, "active", false, "only sessions in running or waiting states")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 0, "treat old live sessions as stale without writing")

	return cmd
}

type scanOptions struct {
	state string
}

func (app *application) newScanCommand() *cobra.Command {
	options := scanOptions{state: string(registry.StateUnknown)}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan tmux panes for supported harness processes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runScan(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.state, "state", options.state, "state to use for newly observed panes")

	return cmd
}

func (app *application) runScan(ctx context.Context, options scanOptions) error {
	state, err := registry.NormalizeState(options.state)
	if err != nil {
		return fmt.Errorf("normalizing state: %w", err)
	}

	panes, err := tmuxctx.ListPanes(ctx)
	if err != nil {
		return fmt.Errorf("listing tmux panes: %w", err)
	}

	sessions := make([]registry.Session, 0, len(panes))
	for _, pane := range panes {
		harness, ok := harnesspkg.FromCommand(pane.CurrentCommand)
		if !ok {
			continue
		}

		session, reportErr := app.registryStore().Report(ctx, registry.Report{
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
		if reportErr != nil {
			return fmt.Errorf("reporting scanned pane: %w", reportErr)
		}
		sessions = append(sessions, session)
	}

	if app.outputJSON {
		return app.writeJSON(sessions)
	}

	return app.writef("reported %d session(s)\n", len(sessions))
}

func (app *application) newGCCommand() *cobra.Command {
	var staleAfter time.Duration
	var deleteAfter time.Duration
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Mark old sessions stale and optionally delete stale/exited records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := app.registryStore().GC(cmd.Context(), staleAfter, deleteAfter)
			if err != nil {
				return fmt.Errorf("garbage collecting sessions: %w", err)
			}

			if app.outputJSON {
				return app.writeJSON(result)
			}

			return app.writef("marked_stale=%d deleted=%d remaining=%d\n", result.MarkedStale, result.Deleted, result.Remaining)
		},
	}
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 0, "mark live sessions stale after this age")
	cmd.Flags().DurationVar(&deleteAfter, "delete-after", 0, "delete stale/exited sessions after this age")

	return cmd
}

func buildFilter(options listOptions) (registry.Filter, error) {
	filter := registry.Filter{
		TmuxSession: options.tmuxSession,
		ActiveOnly:  options.activeOnly,
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
		if options.desc {
			reverseSessions(sessions)
		}

		return nil
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

func reverseSessions(sessions []registry.Session) {
	for left, right := 0, len(sessions)-1; left < right; left, right = left+1, right-1 {
		sessions[left], sessions[right] = sessions[right], sessions[left]
	}
}

type sessionCompareFunc func(registry.Session, registry.Session) int

var listSortComparers = map[string]sessionCompareFunc{
	"tmux":          compareSessionTmux,
	"updated":       compareSessionUpdated,
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
	case "time", "updated-at", "updated":
		return "updated"
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
	leftKey := []string{
		left.Tmux.SessionName,
		left.Tmux.WindowIndex,
		left.Tmux.PaneIndex,
		string(left.Harness),
		left.ID,
	}
	rightKey := []string{
		right.Tmux.SessionName,
		right.Tmux.WindowIndex,
		right.Tmux.PaneIndex,
		string(right.Harness),
		right.ID,
	}

	for index := range leftKey {
		if cmp := strings.Compare(leftKey[index], rightKey[index]); cmp != 0 {
			return cmp
		}
	}

	return compareTime(left.UpdatedAt, right.UpdatedAt)
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

func readRawPayload(stdin io.Reader, enabled bool) (json.RawMessage, error) {
	if !enabled {
		return nil, nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("reading stdin payload: %w", err)
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil
	}

	if json.Valid(data) {
		return json.RawMessage(data), nil
	}

	wrapped, err := json.Marshal(string(data))
	if err != nil {
		return nil, fmt.Errorf("encoding stdin payload: %w", err)
	}

	return json.RawMessage(wrapped), nil
}

func applyPayloadDefaults(
	options *reportOptions,
	attributes map[string]string,
	defaults harnesspkg.PayloadDefaults,
) {
	if options.sessionID == "" && defaults.SessionID != "" {
		options.sessionID = defaults.SessionID
	}
	if options.sessionPath == "" && defaults.SessionPath != "" {
		options.sessionPath = defaults.SessionPath
	}
	if options.event == "" && defaults.Event != "" {
		options.event = defaults.Event
	}
	if defaults.CWD != "" && (options.cwd == "" || options.cwdAuto) {
		options.cwd = defaults.CWD
		if options.projectRoot == "" || options.projectRootAuto {
			options.projectRoot = findProjectRoot(defaults.CWD)
			options.projectRootAuto = true
		}
	}
	if defaults.ProjectRoot != "" && (options.projectRoot == "" || options.projectRootAuto) {
		options.projectRoot = defaults.ProjectRoot
		options.projectRootAuto = true
	}
	for key, value := range defaults.Attributes {
		attributes[key] = value
	}
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
