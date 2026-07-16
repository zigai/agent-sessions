package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"time"
)

const (
	storeSchemaVersion      = 2
	storeVersion            = storeSchemaVersion
	maxObservedAtFutureSkew = 5 * time.Minute
)

var (
	ErrSessionNotFound     = errors.New("session not found")
	ErrHarnessRequired     = errors.New("harness is required")
	ErrObservationIdentity = errors.New("observation requires identity")
	ErrObservationConflict = errors.New("observation conflicts with accepted evidence")
)

type UnsupportedSchemaError struct {
	Path    string
	Version int
}

func (e *UnsupportedSchemaError) Error() string {
	version := "missing"
	if e.Version != 0 {
		version = strconv.Itoa(e.Version)
	}
	return fmt.Sprintf("unsupported store schema %s at %s; use manage reset --store %s or move/remove the file", version, e.Path, e.Path)
}

type snapshot struct {
	SchemaVersion int                `json:"schema_version"`
	Version       int                `json:"-"`
	UpdatedAt     time.Time          `json:"updated_at"`
	Sessions      map[string]Session `json:"sessions"`
}

type GCResult struct {
	Deleted   int `json:"deleted"`
	Remaining int `json:"remaining"`
}
type ResetResult struct {
	Cleared   int `json:"cleared"`
	Remaining int `json:"remaining"`
}

type FileStore struct {
	path string
	now  func() time.Time
}

var _ Store = (*FileStore)(nil)

func NewFileStore(path string) *FileStore {
	if path == "" {
		path = DefaultStorePath()
	}
	return &FileStore{path: path, now: func() time.Time { return time.Now().UTC() }}
}

func (s *FileStore) Path() string                       { return s.path }
func (s *FileStore) SetNowForTest(now func() time.Time) { s.now = now }

func (s *FileStore) Observe(ctx context.Context, observation Observation) (Session, error) {
	sessions, err := s.ObserveBatch(ctx, []Observation{observation})
	if err != nil {
		return Session{}, err
	}
	if len(sessions) > 0 {
		return sessions[0], nil
	}
	snap, loadErr := s.load()
	if loadErr != nil {
		return Session{}, loadErr
	}
	id := findMatchingSession(snap.Sessions, observation)
	if id == "" {
		id = sessionIDForObservation(observation)
	}
	return s.Get(ctx, id)
}

//nolint:gocognit,cyclop // batch reduction coordinates identity, timestamp, and evidence precedence
func (s *FileStore) ObserveBatch(ctx context.Context, observations []Observation) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("checking context: %w", err)
	}
	receivedAt := s.now().UTC()
	saved := make([]Session, 0, len(observations))
	err := s.withSnapshot(func(snap *snapshot) error {
		for index := range observations {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("checking context: %w", err)
			}
			observation := observations[index]
			if observation.Harness == "" {
				return ErrHarnessRequired
			}
			if observation.ObservedAt.IsZero() {
				observation.ObservedAt = receivedAt
			}
			if err := ValidateObservation(observation); err != nil {
				return err
			}
			at, _ := observationTime(observation.ObservedAt, receivedAt)
			id := findMatchingSession(snap.Sessions, observation)
			if id == "" && observation.Source == ObservationSourceCatalog && (observation.Harness != HarnessClaude || observation.Catalog == nil || !observation.Catalog.Current) {
				continue
			}
			if id == "" {
				id = sessionIDForObservation(observation)
			}
			session := snap.Sessions[id]
			if session.ID == "" {
				session = newSession(id, observation.Harness, receivedAt)
			} else if session.Harness != observation.Harness {
				id = sessionIDForObservation(observation)
				session = newSession(id, observation.Harness, receivedAt)
			}
			if id != "" && shouldIgnoreNativeAfterGone(session, observation, at) {
				saved = append(saved, session)
				continue
			}
			if err := applyObservation(&session, observation, at, receivedAt); err != nil {
				return err
			}
			snap.Sessions[session.ID] = session
			saved = append(saved, session)
		}
		snap.UpdatedAt = maxTime(snap.UpdatedAt, receivedAt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

func newSession(id string, harness Harness, now time.Time) Session {
	activity := ActivityUnknown
	return Session{
		SchemaVersion:     storeSchemaVersion,
		ID:                id,
		Harness:           harness,
		Presence:          PresenceUnknown,
		Activity:          &activity,
		SessionID:         "",
		SessionPath:       "",
		ResumeCommand:     nil,
		CWD:               "",
		ProjectRoot:       "",
		Process:           nil,
		Tmux:              TmuxContext{},  //nolint:exhaustruct // zero-value location is the new session default
		Observations:      Observations{}, //nolint:exhaustruct // no evidence has been observed yet
		CreatedAt:         now,
		UpdatedAt:         now,
		PresenceChangedAt: time.Time{},
		ActivityChangedAt: time.Time{},
	}
}

func observationTime(observedAt, receivedAt time.Time) (time.Time, bool) {
	if observedAt.IsZero() {
		return receivedAt, true
	}
	observedAt = observedAt.UTC()
	if observedAt.After(receivedAt.Add(maxObservedAtFutureSkew)) {
		return receivedAt, false
	}
	return observedAt, true
}

func sourceSlotTime(session Session, observation Observation) time.Time {
	switch observation.Source {
	case ObservationSourceNative:
		if session.Observations.Native != nil {
			return session.Observations.Native.ObservedAt
		}
	case ObservationSourceProcess:
		if session.Observations.Process != nil {
			return session.Observations.Process.ObservedAt
		}
	case ObservationSourceTmux:
		if session.Observations.Tmux != nil {
			return session.Observations.Tmux.ObservedAt
		}
	case ObservationSourceCatalog:
		if session.Observations.Catalog != nil {
			return session.Observations.Catalog.ObservedAt
		}
	}
	return time.Time{}
}

func existingSlot(session Session, observation Observation) any {
	switch observation.Source {
	case ObservationSourceNative:
		return session.Observations.Native
	case ObservationSourceProcess:
		return session.Observations.Process
	case ObservationSourceTmux:
		return session.Observations.Tmux
	case ObservationSourceCatalog:
		return session.Observations.Catalog
	default:
		return nil
	}
}

func shouldIgnoreNativeAfterGone(session Session, observation Observation, at time.Time) bool {
	if observation.Source != ObservationSourceNative || session.Presence != PresenceGone {
		return false
	}
	if observation.Lifecycle != nil && (*observation.Lifecycle == NativeLifecycleStart || *observation.Lifecycle == NativeLifecycleResume) {
		return false
	}
	return !at.Before(session.PresenceChangedAt)
}

func applyObservation(session *Session, observation Observation, at, receivedAt time.Time) error {
	if previous := sourceSlotTime(*session, observation); !previous.IsZero() {
		if at.Before(previous) {
			return fmt.Errorf("%w: %s observation at %s precedes %s", ErrObservationConflict, observation.Source, at, previous)
		}
		if at.Equal(previous) {
			if observationEquivalent(*session, observation, at) {
				return nil
			}
			return fmt.Errorf("%w: %s observation at %s", ErrObservationConflict, observation.Source, at)
		}
	}
	previousPresence := session.Presence
	previousActivity := session.Activity
	if err := storeObservation(session, observation, at); err != nil {
		return err
	}
	applyIdentity(session, observation)
	applyMetadata(session, observation)
	applyPresenceAndActivity(session, observation, at)
	session.SchemaVersion = storeSchemaVersion
	session.UpdatedAt = maxTime(session.UpdatedAt, receivedAt)
	if session.Presence != previousPresence {
		session.PresenceChangedAt = at
	}
	if !activityEqual(session.Activity, previousActivity) {
		session.ActivityChangedAt = at
	}
	return nil
}

func observationEquivalent(session Session, observation Observation, at time.Time) bool {
	candidate := session
	if err := storeObservation(&candidate, observation, at); err != nil {
		return false
	}
	return reflect.DeepEqual(existingSlot(session, observation), existingSlot(candidate, observation))
}

func storeObservation(session *Session, observation Observation, at time.Time) error {
	switch observation.Source {
	case ObservationSourceNative:
		lifecycle := cloneLifecycle(observation.Lifecycle)
		presence := clonePresence(observation.Presence)
		activity := cloneActivity(observation.Activity)
		session.Observations.Native = &NativeObservation{Event: observation.NativeEvent, Lifecycle: lifecycle, Presence: presence, Activity: activity, SessionID: observation.Identity.SessionID, SessionPath: observation.Identity.SessionPath, ObservedAt: at, Attributes: cloneAttributes(observation.Attributes), RawPayload: cloneRaw(observation.RawPayload)}
	case ObservationSourceProcess:
		present := observation.ProcessPresent != nil && *observation.ProcessPresent
		var process ProcessIdentity
		if observation.Process != nil {
			process = *observation.Process
		}
		session.Observations.Process = &ProcessObservation{Present: present, Process: process, ObservedAt: at}
	case ObservationSourceTmux:
		if observation.Tmux == nil || observation.Process == nil {
			return ErrInvalidObservation
		}
		session.Observations.Tmux = &TmuxObservation{Process: *observation.Process, Context: *observation.Tmux, ObservedAt: at}
	case ObservationSourceCatalog:
		if observation.Catalog == nil {
			return ErrInvalidObservation
		}
		session.Observations.Catalog = &CatalogObservation{SessionID: observation.Identity.SessionID, SessionPath: observation.Identity.SessionPath, ResumeCommand: append([]string(nil), observation.Catalog.ResumeCommand...), CWD: observation.Catalog.CWD, ProjectRoot: observation.Catalog.ProjectRoot, ProcessPID: observation.Catalog.ProcessPID, ObservedAt: at}
	default:
		return ErrUnknownSource
	}
	return nil
}

func applyIdentity(session *Session, observation Observation) {
	if observation.Identity.SessionID != "" {
		session.SessionID = observation.Identity.SessionID
	}
	if observation.Identity.SessionPath != "" {
		session.SessionPath = filepath.Clean(observation.Identity.SessionPath)
	}
	if observation.Source == ObservationSourceNative && session.Observations.Native != nil {
		if session.Observations.Native.SessionID != "" {
			session.SessionID = session.Observations.Native.SessionID
		}
		if session.Observations.Native.SessionPath != "" {
			session.SessionPath = filepath.Clean(session.Observations.Native.SessionPath)
		}
	}
	if observation.Source == ObservationSourceCatalog && session.Observations.Catalog != nil {
		if session.SessionID == "" {
			session.SessionID = session.Observations.Catalog.SessionID
		}
		if session.SessionPath == "" && session.Observations.Catalog.SessionPath != "" {
			session.SessionPath = filepath.Clean(session.Observations.Catalog.SessionPath)
		}
	}
}

func applyMetadata(session *Session, observation Observation) {
	if observation.Process != nil && observation.Process.Complete() {
		process := *observation.Process
		session.Process = &process
		if process.CWD != "" {
			session.CWD = process.CWD
		}
		if observation.Source == ObservationSourceProcess {
			return
		}
	}
	if (observation.Source == ObservationSourceNative || observation.Source == ObservationSourceCatalog) && observation.Catalog != nil {
		applyCatalogMetadata(session, observation.Catalog)
	}
	if observation.Tmux != nil {
		session.Tmux = *observation.Tmux
		if session.CWD == "" {
			session.CWD = observation.Tmux.PaneCurrentPath
		}
	}
}

func applyCatalogMetadata(session *Session, catalog *CatalogMetadata) {
	if session.CWD == "" {
		session.CWD = catalog.CWD
	}
	if session.ProjectRoot == "" {
		session.ProjectRoot = catalog.ProjectRoot
	}
	if len(session.ResumeCommand) == 0 {
		session.ResumeCommand = append([]string(nil), catalog.ResumeCommand...)
	}
}

//nolint:gocognit,cyclop // presence and activity are reduced independently by source precedence
func applyPresenceAndActivity(session *Session, observation Observation, at time.Time) {
	switch observation.Source {
	case ObservationSourceNative:
		native := session.Observations.Native
		if native == nil {
			return
		}
		if native.Lifecycle != nil {
			switch *native.Lifecycle {
			case NativeLifecycleEnd:
				setGone(session, at)
			case NativeLifecycleStart, NativeLifecycleResume:
				if session.Presence == PresenceGone && at.After(session.PresenceChangedAt) {
					session.Presence = PresenceUnknown
					session.Activity = activityPtr(ActivityUnknown)
				}
			}
		}
		if native.Presence != nil {
			switch *native.Presence {
			case PresenceGone:
				setGone(session, at)
			case PresenceLive:
				if !nativeEndAfter(session, at) {
					session.Presence = PresenceLive
				}
			case PresenceUnknown:
				if !nativeEndAfter(session, at) {
					session.Presence = PresenceUnknown
				}
			}
		}
		if native.Activity != nil && session.Presence != PresenceGone && at.After(session.PresenceChangedAt) {
			session.Activity = cloneActivity(native.Activity)
		}
	case ObservationSourceProcess:
		process := session.Observations.Process
		if process == nil {
			return
		}
		if process.Present {
			if !nativeEndAfter(session, at) {
				session.Presence = PresenceLive
				if session.Activity == nil {
					session.Activity = activityPtr(ActivityUnknown)
				}
			}
			return
		}
		setGone(session, at)
	case ObservationSourceTmux, ObservationSourceCatalog:
		return
	}
}

func nativeEndAfter(session *Session, _ time.Time) bool {
	return session.Observations.Native != nil && session.Observations.Native.Lifecycle != nil && *session.Observations.Native.Lifecycle == NativeLifecycleEnd
}

func setGone(session *Session, at time.Time) {
	if session.Presence != PresenceGone {
		session.Presence = PresenceGone
		session.PresenceChangedAt = at
	}
	session.Activity = nil
}

//nolint:cyclop // matching evaluates process, native identity, and catalog evidence in precedence order
func findMatchingSession(sessions map[string]Session, observation Observation) string {
	if observation.Process != nil && observation.Process.Complete() {
		if id := firstMatchingSessionID(sessions, observation.Harness, func(session Session) bool {
			return session.Process != nil && session.Process.Equal(*observation.Process)
		}); id != "" {
			return id
		}
	}
	if observation.Identity.SessionID != "" {
		if id := firstMatchingSessionID(sessions, observation.Harness, func(session Session) bool {
			return session.SessionID == observation.Identity.SessionID
		}); id != "" {
			return id
		}
	}
	if observation.Identity.SessionPath != "" {
		cleanPath := filepath.Clean(observation.Identity.SessionPath)
		if id := firstMatchingSessionID(sessions, observation.Harness, func(session Session) bool {
			return session.SessionPath != "" && filepath.Clean(session.SessionPath) == cleanPath
		}); id != "" {
			return id
		}
	}
	if observation.Source == ObservationSourceCatalog && observation.Catalog != nil && observation.Catalog.ProcessPID > 0 {
		return firstMatchingSessionID(sessions, observation.Harness, func(session Session) bool {
			return session.Process != nil && session.Process.PID == observation.Catalog.ProcessPID
		})
	}
	return ""
}

func firstMatchingSessionID(sessions map[string]Session, harness Harness, match func(Session) bool) string {
	ids := make([]string, 0, 1)
	for id, session := range sessions {
		if session.Harness == harness && match(session) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return ids[0]
}

func cloneLifecycle(value *NativeLifecycle) *NativeLifecycle {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func clonePresence(value *Presence) *Presence {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneActivity(value *Activity) *Activity {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
func activityPtr(value Activity) *Activity { return &value }
func cloneAttributes(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(value))
	maps.Copy(cloned, value)
	return cloned
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func activityEqual(left, right *Activity) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (s *FileStore) List(ctx context.Context, filter Filter) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("checking context: %w", err)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(snap.Sessions))
	for _, session := range snap.Sessions {
		session.SchemaVersion = storeSchemaVersion
		sessions = append(sessions, session)
	}
	return filterSessions(sessions, filter), nil
}

func (s *FileStore) Get(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, fmt.Errorf("checking context: %w", err)
	}
	snap, err := s.load()
	if err != nil {
		return Session{}, err
	}
	session, ok := snap.Sessions[id]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	session.SchemaVersion = storeSchemaVersion
	return session, nil
}

func (s *FileStore) SummaryByTmuxSession(ctx context.Context, filter Filter) ([]Summary, error) {
	return s.SummaryByTmuxSessionWithOptions(ctx, SummaryOptions{Filter: filter})
}

func (s *FileStore) SummaryByTmuxSessionWithOptions(ctx context.Context, options SummaryOptions) ([]Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("checking context: %w", err)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(snap.Sessions))
	for _, session := range snap.Sessions {
		sessions = append(sessions, session)
	}
	return summariesForSessions(filterSessions(sessions, options.Filter)), nil
}

func summariesForSessions(sessions []Session) []Summary {
	byKey := make(map[string]*Summary)
	order := make([]string, 0)
	for _, session := range sessions {
		key := summaryKeyForSession(session)
		summary := byKey[key]
		if summary == nil {
			summary = &Summary{TmuxSessionID: session.Tmux.SessionID, TmuxSessionName: session.Tmux.SessionName, Total: 0, Live: 0, Gone: 0, PresenceUnknown: 0, Running: 0, Waiting: 0, Idle: 0, ActivityUnknown: 0}
			byKey[key] = summary
			order = append(order, key)
		}
		summary.Total++
		switch session.Presence {
		case PresenceLive:
			summary.Live++
		case PresenceGone:
			summary.Gone++
		case PresenceUnknown:
			summary.PresenceUnknown++
		}
		if session.Presence == PresenceGone {
			continue
		}
		switch {
		case session.Activity == nil, *session.Activity == ActivityUnknown:
			summary.ActivityUnknown++
		case *session.Activity == ActivityRunning:
			summary.Running++
		case *session.Activity == ActivityWaiting:
			summary.Waiting++
		case *session.Activity == ActivityIdle:
			summary.Idle++
		}
	}
	result := make([]Summary, 0, len(order))
	for _, key := range order {
		result = append(result, *byKey[key])
	}
	return result
}

func summaryKeyForSession(session Session) string {
	if session.Tmux.SessionID != "" {
		return "tmux:" + session.Tmux.SessionID
	}
	if session.Tmux.SessionName != "" {
		return "tmux-name:" + session.Tmux.SessionName
	}
	return "unknown"
}

func (s *FileStore) GC(ctx context.Context, deleteAfter time.Duration) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, fmt.Errorf("checking context: %w", err)
	}
	now := s.now().UTC()
	result := GCResult{Deleted: 0, Remaining: 0}
	err := s.withSnapshot(func(snap *snapshot) error {
		for id, session := range snap.Sessions {
			if deleteAfter >= 0 && session.Presence == PresenceGone && !session.PresenceChangedAt.IsZero() && now.Sub(session.PresenceChangedAt) >= deleteAfter {
				delete(snap.Sessions, id)
				result.Deleted++
			}
		}
		result.Remaining = len(snap.Sessions)
		snap.UpdatedAt = now
		return nil
	})
	if err != nil {
		return GCResult{}, err
	}
	return result, nil
}

func (s *FileStore) Reset(ctx context.Context) (ResetResult, error) {
	if err := ctx.Err(); err != nil {
		return ResetResult{}, fmt.Errorf("checking context: %w", err)
	}
	now := s.now().UTC()
	result := ResetResult{Cleared: 0, Remaining: 0}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return ResetResult{}, fmt.Errorf("creating state directory: %w", err)
	}
	lock, err := openStoreLock(s.path + ".lock")
	if err != nil {
		return ResetResult{}, err
	}
	defer func() { _ = lock.Close() }()
	old, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ResetResult{}, fmt.Errorf("reading store: %w", err)
	}
	if len(old) > 0 {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(old, &envelope); err != nil {
			return ResetResult{}, fmt.Errorf("parsing store %s: %w", s.path, err)
		}
		if raw, ok := envelope["sessions"]; ok {
			var sessions map[string]json.RawMessage
			if err := json.Unmarshal(raw, &sessions); err == nil {
				result.Cleared = len(sessions)
			}
		}
	}
	snap := newSnapshot()
	snap.UpdatedAt = now
	result.Remaining = 0
	if err := writeSnapshotAtomic(s.path, snap); err != nil {
		return ResetResult{}, err
	}
	return result, nil
}

func (s *FileStore) withSnapshot(mutator func(*snapshot) error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	lock, err := openStoreLock(s.path + ".lock")
	if err != nil {
		return err
	}
	snap, err := s.load()
	if err != nil {
		return closeStoreLock(lock, err)
	}
	if err := mutator(&snap); err != nil {
		return closeStoreLock(lock, err)
	}
	return closeStoreLock(lock, writeSnapshotAtomic(s.path, snap))
}

func closeStoreLock(lock *storeLock, err error) error {
	if closeErr := lock.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func (s *FileStore) load() (snapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newSnapshot(), nil
		}
		return snapshot{}, fmt.Errorf("reading store: %w", err)
	}
	var header struct {
		SchemaVersion *int `json:"schema_version"`
		Version       *int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return snapshot{}, fmt.Errorf("parsing store %s: %w", s.path, err)
	}
	if header.SchemaVersion == nil {
		version := 0
		if header.Version != nil {
			version = *header.Version
		}
		return snapshot{}, &UnsupportedSchemaError{Path: s.path, Version: version}
	}
	if *header.SchemaVersion != storeSchemaVersion {
		return snapshot{}, &UnsupportedSchemaError{Path: s.path, Version: *header.SchemaVersion}
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return snapshot{}, fmt.Errorf("parsing store %s: %w", s.path, err)
	}
	if snap.Sessions == nil {
		snap.Sessions = make(map[string]Session)
	}
	snap.SchemaVersion = storeSchemaVersion
	return snap, nil
}

func newSnapshot() snapshot {
	return snapshot{SchemaVersion: storeSchemaVersion, Version: 0, UpdatedAt: time.Time{}, Sessions: make(map[string]Session)}
}

func writeSnapshotAtomic(path string, snap snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding store: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp store: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if keep {
			return
		}
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("setting temp store permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("writing temp store: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("syncing temp store: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing temp store: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("renaming temp store: %w", err)
	}
	keep = true
	return syncDir(dir)
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening state directory: %w", err)
	}
	defer func() { _ = handle.Close() }()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("syncing state directory: %w", err)
	}
	return nil
}
