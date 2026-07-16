package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const defaultWatchDebounce = 100 * time.Millisecond
const (
	watchFormatTable = "table"
	watchFormatPlain = "plain"
	watchFormatJSON  = "json"
)

const (
	watchActionAdded           = "added"
	watchActionRemoved         = "removed"
	watchActionSnapshot        = "snapshot"
	watchActionSnapshotEmpty   = "snapshot_empty"
	watchActionPresenceChanged = "presence_changed"
	watchActionActivityChanged = "activity_changed"
	watchActionProcessBound    = "process_bound"
	watchActionProcessGone     = "process_gone"
	watchActionLocationChanged = "location_changed"
	watchActionNativeEvent     = "native_event"
)

const (
	watchActivityOrder = 2
	watchProcessOrder  = watchActivityOrder + 1
	watchLocationOrder = watchProcessOrder + 1
	watchNativeOrder   = watchLocationOrder + 1
)

var (
	errWatchFormatJSONConflict    = errors.New("--format cannot be used with --json")
	errWatchStateDirectoryMissing = errors.New("watching store: state directory does not exist")
	errWatchStateDirectoryNotDir  = errors.New("watching store: state directory is not a directory")
	errInvalidWatchFormat         = errors.New("invalid watch format")
)

type watchOptions struct {
	filter              registry.Filter
	summary, noSnapshot bool
	format              string
	formatSet           bool
	debounce            time.Duration
	now                 func() time.Time
	ready               chan struct{}
}
type watchEvent struct {
	Time             time.Time          `json:"time"`
	Action           string             `json:"action"`
	ID               string             `json:"id,omitempty"`
	Harness          registry.Harness   `json:"harness,omitempty"`
	Presence         registry.Presence  `json:"presence,omitempty"`
	PreviousPresence registry.Presence  `json:"previous_presence,omitempty"`
	Activity         *registry.Activity `json:"activity"`
	PreviousActivity *registry.Activity `json:"previous_activity"`
	SessionID        string             `json:"session_id,omitempty"`
	SessionPath      string             `json:"session_path,omitempty"`
	Label            string             `json:"label,omitempty"`
	NativeEvent      string             `json:"native_event,omitempty"`
	CWD              string             `json:"cwd,omitempty"`
	Tmux             string             `json:"tmux,omitempty"`
}

//nolint:gocognit,cyclop // watch orchestration keeps filesystem, registry, and timer transitions together
func (app *application) runWatch(ctx context.Context, o watchOptions) error {
	if o.formatSet && strings.TrimSpace(o.format) == "" {
		return fmt.Errorf("%w: empty value", errInvalidWatchFormat)
	}
	o = normalizeWatchOptions(o)
	if app.outputJSON {
		if o.formatSet {
			return errWatchFormatJSONConflict
		}
		o.format = watchFormatJSON
	}
	if !app.outputJSON && o.format != watchFormatTable && o.format != watchFormatPlain {
		return fmt.Errorf("%w: %q", errInvalidWatchFormat, o.format)
	}
	s := app.store()
	target, dir, e := watchTarget(s)
	if e != nil {
		return e
	}
	w, e := fsnotify.NewWatcher()
	if e != nil {
		return fmt.Errorf("create watcher: %w", e)
	}
	defer func() { _ = w.Close() }()
	if e = w.Add(dir); e != nil {
		return fmt.Errorf("watch state directory: %w", e)
	}
	prev, err := s.List(ctx, o.filter)
	if err != nil {
		return fmt.Errorf("list sessions for watch: %w", err)
	}
	if !o.noSnapshot {
		if e = app.writeWatchEvents(snapshotWatchEvents(prev, o.now()), o.format); e != nil {
			return e
		}
	}
	notifyWatchReady(o.ready)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !isRelevantWatchEvent(ev, target) {
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(o.debounce)
		case <-timer.C:
			next, er := s.List(ctx, o.filter)
			if er != nil {
				app.warnf("watch warning: %v\n", er)
				continue
			}
			if e = app.writeWatchEvents(diffWatchEvents(watchSessionMap(prev), watchSessionMap(next), o.now()), o.format); e != nil {
				return e
			}
			prev = next
		case er := <-w.Errors:
			if er != nil {
				app.warnf("watch warning: %v\n", er)
			}
		}
	}
}

func normalizeWatchOptions(o watchOptions) watchOptions {
	if strings.TrimSpace(o.format) == "" {
		o.format = watchFormatTable
	}
	if o.debounce <= 0 {
		o.debounce = defaultWatchDebounce
	}
	if o.now == nil {
		o.now = func() time.Time { return time.Now().UTC() }
	}
	return o
}

func watchTarget(s *registry.FileStore) (string, string, error) {
	p, e := filepath.Abs(s.Path())
	if e != nil {
		return "", "", fmt.Errorf("resolve watch target: %w", e)
	}
	d := filepath.Dir(p)
	i, e := os.Stat(d)
	if e != nil {
		if os.IsNotExist(e) {
			return "", "", fmt.Errorf("%w: %s", errWatchStateDirectoryMissing, d)
		}
		return "", "", fmt.Errorf("stat watch state directory: %w", e)
	}
	if !i.IsDir() {
		return "", "", fmt.Errorf("%w: %s", errWatchStateDirectoryNotDir, d)
	}
	return p, d, nil
}

func notifyWatchReady(c chan struct{}) {
	if c != nil {
		close(c)
	}
}

func isRelevantWatchEvent(e fsnotify.Event, target string) bool {
	if e.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}
	p, _ := filepath.Abs(e.Name)
	return filepath.Clean(p) == filepath.Clean(target)
}

func watchSessionMap(s []registry.Session) map[string]registry.Session {
	m := map[string]registry.Session{}
	for _, v := range s {
		m[v.ID] = v
	}
	return m
}

func snapshotWatchEvents(s []registry.Session, at time.Time) []watchEvent {
	if len(s) == 0 {
		return []watchEvent{{Time: at.UTC(), Action: watchActionSnapshotEmpty}}
	}
	o := []watchEvent{}
	for _, v := range s {
		o = append(o, watchEventFromSession(watchActionSnapshot, v, registry.Session{}, at))
	}
	sortWatchEvents(o)
	return o
}

//nolint:gocognit,cyclop // event diffing compares each independent v2 dimension
func diffWatchEvents(p, n map[string]registry.Session, at time.Time) []watchEvent {
	o := []watchEvent{}
	for id, v := range n {
		old, ok := p[id]
		if !ok {
			o = append(o, watchEventFromSession(watchActionAdded, v, registry.Session{}, at))
			continue
		}
		if v.Presence != old.Presence {
			o = append(o, watchEventFromSession(watchActionPresenceChanged, v, old, at))
			if v.Presence == registry.PresenceLive && v.Process != nil && old.Process == nil {
				o = append(o, watchEventFromSession(watchActionProcessBound, v, old, at))
			}
			if v.Presence == registry.PresenceGone {
				o = append(o, watchEventFromSession(watchActionProcessGone, v, old, at))
			}
		}
		if !activityEqual(v.Activity, old.Activity) {
			o = append(o, watchEventFromSession(watchActionActivityChanged, v, old, at))
		}
		if v.Tmux != old.Tmux {
			o = append(o, watchEventFromSession(watchActionLocationChanged, v, old, at))
		}
		if nativeEvent(v) != nativeEvent(old) {
			o = append(o, watchEventFromSession(watchActionNativeEvent, v, old, at))
		}
	}
	for id, v := range p {
		if _, ok := n[id]; !ok {
			o = append(o, watchEventFromSession(watchActionRemoved, v, registry.Session{}, at))
		}
	}
	sortWatchEvents(o)
	return o
}

func activityEqual(a, b *registry.Activity) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func nativeEvent(s registry.Session) string {
	if s.Observations.Native == nil {
		return ""
	}
	return s.Observations.Native.Event
}

func watchEventFromSession(a string, s, p registry.Session, at time.Time) watchEvent {
	e := watchEvent{Time: at.UTC(), Action: a, ID: s.ID, Harness: s.Harness, Presence: s.Presence, Activity: s.Activity, SessionID: s.SessionID, SessionPath: s.SessionPath, Label: watchSessionLabel(s), NativeEvent: nativeEvent(s), CWD: s.CWD, Tmux: watchTmuxLabel(s.Tmux)}
	if !s.UpdatedAt.IsZero() {
		e.Time = s.UpdatedAt
	}
	if a == watchActionPresenceChanged {
		e.Time = s.PresenceChangedAt
		e.PreviousPresence = p.Presence
	}
	if a == watchActionActivityChanged {
		e.Time = s.ActivityChangedAt
		e.PreviousActivity = p.Activity
	}
	if e.Time.IsZero() {
		e.Time = at.UTC()
	}
	return e
}

func sortWatchEvents(e []watchEvent) {
	order := map[string]int{watchActionPresenceChanged: watchActivityOrder - 1, watchActionActivityChanged: watchActivityOrder, watchActionProcessBound: watchProcessOrder, watchActionProcessGone: watchProcessOrder, watchActionLocationChanged: watchLocationOrder, watchActionNativeEvent: watchNativeOrder}
	sort.SliceStable(e, func(i, j int) bool {
		if !e[i].Time.Equal(e[j].Time) {
			return e[i].Time.Before(e[j].Time)
		}
		if e[i].ID != e[j].ID {
			return e[i].ID < e[j].ID
		}
		return order[e[i].Action] < order[e[j].Action]
	})
}

func watchSessionLabel(s registry.Session) string {
	if s.SessionID != "" {
		return s.SessionID
	}
	if s.SessionPath != "" {
		return s.SessionPath
	}
	if s.Tmux.PaneID != "" {
		return s.Tmux.PaneID
	}
	return s.ID
}

func watchTmuxLabel(c registry.TmuxContext) string {
	p := []string{}
	if x := tmuxSessionLabel(c); x != "-" {
		p = append(p, x)
	}
	if x := tmuxWindowLabel(c); x != "-" {
		p = append(p, x)
	}
	if c.PaneID != "" {
		p = append(p, c.PaneID)
	}
	return strings.Join(p, ":")
}

func (app *application) writeWatchEvents(e []watchEvent, f string) error {
	for _, v := range e {
		switch f {
		case watchFormatJSON:
			if er := app.writeJSONLine(v); er != nil {
				return er
			}
		case watchFormatPlain:
			if er := app.writeln(formatWatchPlainEvent(v)); er != nil {
				return er
			}
		default:
			if er := app.writeln(formatWatchTableEvent(v)); er != nil {
				return er
			}
		}
	}
	return nil
}

func formatWatchPlainEvent(e watchEvent) string {
	return strings.Join([]string{e.Time.UTC().Format(time.RFC3339), e.Action, string(e.Harness), string(e.Presence), appReportActivity(registry.Session{Activity: e.Activity}), "session=" + e.Label}, " ")
}

func formatWatchTableEvent(e watchEvent) string {
	return fmt.Sprintf("%s  %-18s  %-10s  %-8s  %-8s  %s", e.Time.UTC().Format(time.RFC3339), e.Action, e.Harness, e.Presence, appReportActivity(registry.Session{Activity: e.Activity}), e.Label)
}
