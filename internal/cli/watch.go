package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const defaultWatchDebounce = 100 * time.Millisecond

const (
	watchFormatTable = "table"
	watchFormatPlain = "plain"
	watchFormatJSON  = "json"
)

const (
	watchActionAdded         = "added"
	watchActionEventChanged  = "event_changed"
	watchActionRemoved       = "removed"
	watchActionSnapshot      = "snapshot"
	watchActionSnapshotEmpty = "snapshot_empty"
	watchActionStateChanged  = "state_changed"
	watchActionUpdated       = "updated"
)

const (
	watchTmuxLabelPartCapacity = 3
	watchActionColumnWidth     = 14
	watchHarnessColumnWidth    = 10
	watchStateColumnWidth      = 8
	watchLabelColumnWidth      = 24
)

var (
	errWatchFormatJSONConflict    = errors.New("watch --format cannot be used with --json")
	errWatchStateDirectoryMissing = errors.New("watching store: state directory does not exist")
	errWatchStateDirectoryNotDir  = errors.New("watching store: state directory is not a directory")
	errInvalidWatchFormat         = errors.New("invalid watch format")
)

type watchOptions struct {
	noSnapshot bool
	format     string
	formatSet  bool
	debounce   time.Duration
	now        func() time.Time
	ready      chan struct{}
}

type watchEvent struct {
	Time          time.Time        `json:"time"`
	Action        string           `json:"action"`
	ID            string           `json:"id,omitempty"`
	Harness       registry.Harness `json:"harness,omitempty"`
	State         registry.State   `json:"state,omitempty"`
	PreviousState registry.State   `json:"previous_state,omitempty"`
	SessionID     string           `json:"session_id,omitempty"`
	SessionPath   string           `json:"session_path,omitempty"`
	Label         string           `json:"label,omitempty"`
	Event         string           `json:"event,omitempty"`
	PreviousEvent string           `json:"previous_event,omitempty"`
	CWD           string           `json:"cwd,omitempty"`
	Tmux          string           `json:"tmux,omitempty"`
}

func (app *application) newWatchCommand() *cobra.Command {
	options := watchOptions{}
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch registry state changes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			options.formatSet = cmd.Flags().Changed("format")

			return app.runWatch(cmd.Context(), options)
		},
	}
	cmd.Flags().BoolVar(&options.noSnapshot, "no-snapshot", false, "suppress the startup snapshot")
	cmd.Flags().StringVar(&options.format, "format", "", "text output format: table, plain")

	return cmd
}

func (app *application) runWatch(ctx context.Context, options watchOptions) error {
	normalizedOptions, err := app.normalizeWatchRunOptions(options)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	store := app.store()
	target, dir, err := watchTarget(store)
	if err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating file watcher: %w", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	addErr := watcher.Add(dir)
	if addErr != nil {
		return fmt.Errorf("watching state directory: %w", addErr)
	}

	sessions, err := store.List(ctx, registry.Filter{})
	if err != nil {
		return fmt.Errorf("loading initial snapshot: %w", err)
	}
	previous := watchSessionMap(sessions)

	if !normalizedOptions.noSnapshot {
		writeErr := app.writeWatchEvents(
			snapshotWatchEvents(sessions, normalizedOptions.now()),
			normalizedOptions.format,
		)
		if writeErr != nil {
			return writeErr
		}
	}
	notifyWatchReady(normalizedOptions.ready)

	return app.watchStore(ctx, watcher, store, target, previous, normalizedOptions)
}

func (app *application) normalizeWatchRunOptions(options watchOptions) (watchOptions, error) {
	options = normalizeWatchOptions(options)
	if app.outputJSON {
		if options.formatSet {
			return watchOptions{}, errWatchFormatJSONConflict
		}
		options.format = watchFormatJSON
	} else {
		if err := validateWatchTextFormat(options.format); err != nil {
			return watchOptions{}, err
		}
	}
	if err := validateWatchOutputFormat(options.format); err != nil {
		return watchOptions{}, err
	}

	return options, nil
}

func watchTarget(store *registry.FileStore) (string, string, error) {
	target, err := filepath.Abs(store.Path())
	if err != nil {
		return "", "", fmt.Errorf("resolving store path: %w", err)
	}
	dir := filepath.Dir(target)
	info, statErr := os.Stat(dir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", "", fmt.Errorf("%w: %s", errWatchStateDirectoryMissing, dir)
		}

		return "", "", fmt.Errorf("checking state directory: %w", statErr)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("%w: %s", errWatchStateDirectoryNotDir, dir)
	}

	return target, dir, nil
}

func normalizeWatchOptions(options watchOptions) watchOptions {
	if strings.TrimSpace(options.format) == "" {
		options.format = watchFormatTable
	}
	options.format = strings.ToLower(strings.TrimSpace(options.format))
	if options.debounce <= 0 {
		options.debounce = defaultWatchDebounce
	}
	if options.now == nil {
		options.now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return options
}

func validateWatchTextFormat(format string) error {
	switch format {
	case watchFormatTable, watchFormatPlain:
		return nil
	default:
		return fmt.Errorf("%w: %q; use table or plain, or pass global --json for JSON", errInvalidWatchFormat, format)
	}
}

func validateWatchOutputFormat(format string) error {
	switch format {
	case watchFormatTable, watchFormatPlain, watchFormatJSON:
		return nil
	default:
		return validateWatchTextFormat(format)
	}
}

func notifyWatchReady(ready chan struct{}) {
	if ready != nil {
		close(ready)
	}
}

func (app *application) watchStore(
	ctx context.Context,
	watcher *fsnotify.Watcher,
	store *registry.FileStore,
	target string,
	previous map[string]registry.Session,
	options watchOptions,
) error {
	loop := watchLoop{
		app:      app,
		watcher:  watcher,
		store:    store,
		target:   target,
		previous: previous,
		options:  options,
	}
	defer loop.stopTimer()

	return loop.run(ctx)
}

type watchLoop struct {
	app      *application
	watcher  *fsnotify.Watcher
	store    *registry.FileStore
	target   string
	previous map[string]registry.Session
	options  watchOptions
	timer    *time.Timer
	timerC   <-chan time.Time
	pending  bool
}

func (loop *watchLoop) run(ctx context.Context) error {
	for {
		done, err := loop.next(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (loop *watchLoop) next(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return true, nil
	case event, ok := <-loop.watcher.Events:
		if !ok {
			return true, nil
		}
		loop.handleEvent(event)

		return false, nil
	case watchErr, ok := <-loop.watcher.Errors:
		if !ok {
			return true, nil
		}
		loop.handleError(watchErr)

		return false, nil
	case <-loop.timerC:
		if err := loop.reload(ctx); err != nil {
			return false, err
		}

		return false, nil
	}
}

func (loop *watchLoop) handleEvent(event fsnotify.Event) {
	if isRelevantWatchEvent(event, loop.target) {
		loop.scheduleReload()
	}
}

func (loop *watchLoop) handleError(err error) {
	if err != nil {
		loop.app.warnf("watch warning: %v\n", err)
	}
}

func (loop *watchLoop) scheduleReload() {
	if loop.timer == nil {
		loop.timer = time.NewTimer(loop.options.debounce)
		loop.timerC = loop.timer.C
		loop.pending = true

		return
	}

	if !loop.timer.Stop() && loop.pending {
		select {
		case <-loop.timer.C:
		default:
		}
	}
	loop.timer.Reset(loop.options.debounce)
	loop.timerC = loop.timer.C
	loop.pending = true
}

func (loop *watchLoop) reload(ctx context.Context) error {
	loop.pending = false
	loop.timerC = nil
	loop.stopTimer()

	nextSessions, err := loop.store.List(ctx, registry.Filter{})
	if err != nil {
		loop.app.warnf("watch warning: %v\n", err)

		return nil
	}
	next := watchSessionMap(nextSessions)
	events := diffWatchEvents(loop.previous, next, loop.options.now())
	writeErr := loop.app.writeWatchEvents(events, loop.options.format)
	if writeErr != nil {
		return writeErr
	}
	loop.previous = next

	return nil
}

func (loop *watchLoop) stopTimer() {
	if loop.timer != nil {
		loop.timer.Stop()
	}
}

func isRelevantWatchEvent(event fsnotify.Event, target string) bool {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}

	eventPath, err := filepath.Abs(event.Name)
	if err != nil {
		eventPath = filepath.Clean(event.Name)
	}

	return filepath.Clean(eventPath) == filepath.Clean(target)
}

func watchSessionMap(sessions []registry.Session) map[string]registry.Session {
	mapped := make(map[string]registry.Session, len(sessions))
	for _, session := range sessions {
		mapped[session.ID] = session
	}

	return mapped
}

func snapshotWatchEvents(sessions []registry.Session, observedAt time.Time) []watchEvent {
	if len(sessions) == 0 {
		return []watchEvent{{
			Time:   observedAt.UTC(),
			Action: watchActionSnapshotEmpty,
		}}
	}

	events := make([]watchEvent, 0, len(sessions))
	for _, session := range sessions {
		events = append(events, watchEventFromSession(watchActionSnapshot, session, registry.Session{}, observedAt))
	}
	sortWatchEvents(events)

	return events
}

func diffWatchEvents(
	previous map[string]registry.Session,
	next map[string]registry.Session,
	observedAt time.Time,
) []watchEvent {
	events := make([]watchEvent, 0)
	for id, nextSession := range next {
		previousSession, ok := previous[id]
		if !ok {
			events = append(events, watchEventFromSession(watchActionAdded, nextSession, registry.Session{}, observedAt))

			continue
		}

		switch {
		case nextSession.State != previousSession.State:
			events = append(events, watchEventFromSession(watchActionStateChanged, nextSession, previousSession, observedAt))
		case nextSession.LastEvent != previousSession.LastEvent || !nextSession.LastEventAt.Equal(previousSession.LastEventAt):
			events = append(events, watchEventFromSession(watchActionEventChanged, nextSession, previousSession, observedAt))
		case !reflect.DeepEqual(nextSession, previousSession):
			events = append(events, watchEventFromSession(watchActionUpdated, nextSession, previousSession, observedAt))
		}
	}

	for id, previousSession := range previous {
		if _, ok := next[id]; ok {
			continue
		}
		events = append(events, watchEventFromSession(watchActionRemoved, previousSession, registry.Session{}, observedAt))
	}

	sortWatchEvents(events)

	return events
}

func watchEventFromSession(
	action string,
	session registry.Session,
	previous registry.Session,
	observedAt time.Time,
) watchEvent {
	event := watchEvent{
		Time:        watchEventTime(action, session, observedAt),
		Action:      action,
		ID:          session.ID,
		Harness:     session.Harness,
		State:       session.State,
		SessionID:   session.SessionID,
		SessionPath: session.SessionPath,
		Label:       watchSessionLabel(session),
		Event:       session.LastEvent,
		CWD:         session.CWD,
		Tmux:        watchTmuxLabel(session.Tmux),
	}
	if action == watchActionStateChanged {
		event.PreviousState = previous.State
	}
	if action == watchActionEventChanged {
		event.PreviousEvent = previous.LastEvent
	}

	return event
}

func watchEventTime(action string, session registry.Session, observedAt time.Time) time.Time {
	switch action {
	case watchActionRemoved, watchActionSnapshotEmpty:
		return observedAt.UTC()
	case watchActionStateChanged:
		if !session.StateChangedAt.IsZero() {
			return session.StateChangedAt.UTC()
		}
	case watchActionEventChanged:
		if !session.LastEventAt.IsZero() {
			return session.LastEventAt.UTC()
		}
	case watchActionAdded, watchActionSnapshot, watchActionUpdated:
		if !session.UpdatedAt.IsZero() {
			return session.UpdatedAt.UTC()
		}
	}
	if !session.LastSeenAt.IsZero() {
		return session.LastSeenAt.UTC()
	}
	if !session.CreatedAt.IsZero() {
		return session.CreatedAt.UTC()
	}

	return observedAt.UTC()
}

func sortWatchEvents(events []watchEvent) {
	sort.SliceStable(events, func(i int, j int) bool {
		if !events[i].Time.Equal(events[j].Time) {
			return events[i].Time.Before(events[j].Time)
		}
		if events[i].ID != events[j].ID {
			return events[i].ID < events[j].ID
		}

		return events[i].Action < events[j].Action
	})
}

func watchSessionLabel(session registry.Session) string {
	switch {
	case session.SessionID != "":
		return session.SessionID
	case session.SessionPath != "":
		return session.SessionPath
	case session.Tmux.PaneID != "":
		return session.Tmux.PaneID
	default:
		return session.ID
	}
}

func watchTmuxLabel(ctx registry.TmuxContext) string {
	parts := make([]string, 0, watchTmuxLabelPartCapacity)
	if session := tmuxSessionLabel(ctx); session != "-" {
		parts = append(parts, session)
	}
	if window := tmuxWindowLabel(ctx); window != "-" {
		parts = append(parts, window)
	}
	if ctx.PaneID != "" {
		parts = append(parts, ctx.PaneID)
	}

	return strings.Join(parts, ":")
}

func (app *application) writeWatchEvents(events []watchEvent, format string) error {
	for _, event := range events {
		switch format {
		case watchFormatJSON:
			if err := app.writeWatchEventJSON(event); err != nil {
				return err
			}
		case watchFormatPlain:
			if err := app.writeln(formatWatchPlainEvent(event)); err != nil {
				return err
			}
		case watchFormatTable:
			if err := app.writeln(formatWatchTableEvent(event)); err != nil {
				return err
			}
		default:
			return validateWatchOutputFormat(format)
		}
	}

	return nil
}

func (app *application) writeWatchEventJSON(event watchEvent) error {
	encoder := json.NewEncoder(app.stdout)
	if err := encoder.Encode(event); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

func formatWatchPlainEvent(event watchEvent) string {
	parts := []string{
		event.Time.UTC().Format(time.RFC3339),
		event.Action,
	}
	if event.Harness != "" {
		parts = append(parts, string(event.Harness))
	}
	if event.State != "" {
		parts = append(parts, string(event.State))
	}

	fields := []struct {
		key   string
		value string
	}{
		{key: "session", value: event.Label},
		{key: "prev", value: string(event.PreviousState)},
		{key: "event", value: event.Event},
		{key: "prev_event", value: event.PreviousEvent},
		{key: "cwd", value: event.CWD},
		{key: "tmux", value: event.Tmux},
	}
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		parts = append(parts, formatWatchTextField(field.key, field.value))
	}

	return strings.Join(parts, " ")
}

func formatWatchTableEvent(event watchEvent) string {
	details := formatWatchDetails(event)
	columns := []string{
		event.Time.UTC().Format(time.RFC3339),
		padWatchColumn(event.Action, watchActionColumnWidth),
		padWatchColumn(string(event.Harness), watchHarnessColumnWidth),
		padWatchColumn(string(event.State), watchStateColumnWidth),
		padWatchColumn(event.Label, watchLabelColumnWidth),
		details,
	}

	return strings.TrimRight(strings.Join(columns, "  "), " ")
}

func padWatchColumn(value string, width int) string {
	if value == "" {
		value = "-"
	}
	if len(value) >= width {
		return value
	}

	return value + strings.Repeat(" ", width-len(value))
}

func formatWatchDetails(event watchEvent) string {
	fields := []struct {
		key   string
		value string
	}{
		{key: "prev", value: string(event.PreviousState)},
		{key: "event", value: event.Event},
		{key: "prev_event", value: event.PreviousEvent},
		{key: "cwd", value: event.CWD},
		{key: "tmux", value: event.Tmux},
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		parts = append(parts, formatWatchTextField(field.key, field.value))
	}

	return strings.Join(parts, " ")
}

func formatWatchTextField(key string, value string) string {
	if strings.ContainsAny(value, " \t\r\n") {
		return key + "=" + strconv.Quote(value)
	}

	return key + "=" + value
}

func (app *application) warnf(format string, args ...any) {
	if app.stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(app.stderr, format, args...)
}
