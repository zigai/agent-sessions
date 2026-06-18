package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

const storeVersion = 1

var (
	// ErrSessionNotFound is returned when a session id is not present.
	ErrSessionNotFound = errors.New("session not found")
	// ErrHarnessRequired is returned when a report has no harness.
	ErrHarnessRequired = errors.New("harness is required")
	// ErrReportIdentityRequired is returned when a report cannot identify a session.
	ErrReportIdentityRequired = errors.New("report requires state, session id, or session path")
)

type snapshot struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updated_at"`
	Sessions  map[string]Session `json:"sessions"`
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

	return &FileStore{
		path: path,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (s *FileStore) Path() string {
	return s.path
}

func (s *FileStore) SetNowForTest(now func() time.Time) {
	s.now = now
}

func (s *FileStore) Report(ctx context.Context, report Report) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, fmt.Errorf("checking context: %w", err)
	}

	now := observedAtOrNow(report.ObservedAt, s.now)
	var saved Session
	err := s.withSnapshot(func(snap *snapshot) error {
		if report.Harness == "" {
			return ErrHarnessRequired
		}
		if report.State == "" && report.SessionID == "" && report.SessionPath == "" {
			return ErrReportIdentityRequired
		}

		saved = mergeReportSession(snap, report, now)
		snap.Sessions[saved.ID] = saved
		snap.UpdatedAt = maxTime(snap.UpdatedAt, saved.UpdatedAt, now)

		return nil
	})
	if err != nil {
		return Session{}, err
	}

	return saved, nil
}

func mergeReportSession(snap *snapshot, report Report, now time.Time) Session {
	canonicalID := sessionIDForReport(report)
	matchingIDs := matchingSessionIDs(snap.Sessions, report, canonicalID)
	base := mergedExistingSessions(snap.Sessions, matchingIDs, canonicalID)
	saved := mergeReport(base, report, now)

	for _, id := range matchingIDs {
		delete(snap.Sessions, id)
	}

	return saved
}

func matchingSessionIDs(sessions map[string]Session, report Report, canonicalID string) []string {
	matchingIDs := make([]string, 0, 1)
	for id, session := range sessions {
		if id == canonicalID || sessionMatchesReportIdentity(session, report) {
			matchingIDs = append(matchingIDs, id)
		}
	}
	sort.Strings(matchingIDs)

	return matchingIDs
}

func sessionMatchesReportIdentity(session Session, report Report) bool {
	if session.Harness != report.Harness {
		return false
	}
	if report.SessionID != "" && session.SessionID == report.SessionID {
		return true
	}
	if report.SessionPath != "" && session.SessionPath != "" &&
		filepath.Clean(session.SessionPath) == filepath.Clean(report.SessionPath) {
		return true
	}
	if report.Tmux.PaneID != "" && session.Tmux.PaneID == report.Tmux.PaneID {
		return true
	}
	if report.PID > 0 && session.PID == report.PID {
		return true
	}

	return false
}

func mergedExistingSessions(sessions map[string]Session, ids []string, canonicalID string) Session {
	if len(ids) == 0 {
		var empty Session

		return empty
	}

	existing := make([]Session, 0, len(ids))
	for _, id := range ids {
		existing = append(existing, sessions[id])
	}
	sort.Slice(existing, func(i int, j int) bool {
		if !existing[i].UpdatedAt.Equal(existing[j].UpdatedAt) {
			return existing[i].UpdatedAt.Before(existing[j].UpdatedAt)
		}
		if !existing[i].CreatedAt.Equal(existing[j].CreatedAt) {
			return existing[i].CreatedAt.Before(existing[j].CreatedAt)
		}

		return existing[i].ID < existing[j].ID
	})

	var merged Session
	merged.ID = canonicalID
	for _, session := range existing {
		merged = mergeStoredSession(merged, session)
	}
	merged.ID = canonicalID

	return merged
}

func mergeStoredSession(merged Session, session Session) Session {
	merged = mergeStoredLifecycle(merged, session)
	merged = mergeStoredIdentity(merged, session)
	merged = mergeStoredLocation(merged, session)
	merged = mergeStoredMetadata(merged, session)
	merged.UpdatedAt = maxTime(merged.UpdatedAt, session.UpdatedAt)
	merged.LastSeenAt = maxTime(merged.LastSeenAt, session.LastSeenAt)
	merged.EndedAt = maxTime(merged.EndedAt, session.EndedAt)

	return merged
}

func mergeStoredLifecycle(merged Session, session Session) Session {
	if merged.CreatedAt.IsZero() || (!session.CreatedAt.IsZero() && session.CreatedAt.Before(merged.CreatedAt)) {
		merged.CreatedAt = session.CreatedAt
	}
	if session.Harness != "" {
		merged.Harness = session.Harness
	}
	if session.State != "" && !session.StateChangedAt.Before(merged.StateChangedAt) {
		merged.State = session.State
		merged.StateChangedAt = session.StateChangedAt
	}
	if session.LastEvent != "" && !session.LastEventAt.Before(merged.LastEventAt) {
		merged.LastEvent = session.LastEvent
		merged.LastEventAt = session.LastEventAt
	}

	return merged
}

func mergeStoredIdentity(merged Session, session Session) Session {
	if session.SessionID != "" {
		merged.SessionID = session.SessionID
	}
	if session.SessionPath != "" {
		merged.SessionPath = session.SessionPath
	}
	if len(session.ResumeCommand) > 0 {
		merged.ResumeCommand = slices.Clone(session.ResumeCommand)
	}

	return merged
}

func mergeStoredLocation(merged Session, session Session) Session {
	if session.CWD != "" {
		merged.CWD = session.CWD
	}
	if session.ProjectRoot != "" {
		merged.ProjectRoot = session.ProjectRoot
	}
	if session.PID > 0 {
		merged.PID = session.PID
	}
	if session.PPID > 0 {
		merged.PPID = session.PPID
	}
	if session.TTY != "" {
		merged.TTY = session.TTY
	}
	if !session.Tmux.Empty() {
		merged.Tmux = session.Tmux
	}

	return merged
}

func mergeStoredMetadata(merged Session, session Session) Session {
	if session.Source != "" {
		merged.Source = session.Source
	}
	if session.Confidence != "" {
		merged.Confidence = session.Confidence
	}
	if len(session.Attributes) > 0 {
		merged.Attributes = mergeAttributes(merged.Attributes, session.Attributes)
	}
	if len(session.RawPayload) > 0 {
		merged.RawPayload = append(json.RawMessage(nil), session.RawPayload...)
	}

	return merged
}

func (s *FileStore) List(ctx context.Context, filter Filter) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("checking context: %w", err)
	}

	sessions, err := s.listAll()
	if err != nil {
		return nil, err
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

	return session, nil
}

func (s *FileStore) SummaryByTmuxSession(ctx context.Context, filter Filter) ([]Summary, error) {
	return s.SummaryByTmuxSessionWithOptions(ctx, SummaryOptions{
		Filter: filter,
	})
}

func (s *FileStore) SummaryByTmuxSessionWithOptions(ctx context.Context, options SummaryOptions) ([]Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("checking context: %w", err)
	}

	sessions, err := s.listAll()
	if err != nil {
		return nil, err
	}

	return summariesForSessions(filterSessions(sessions, options.Filter)), nil
}

func summariesForSessions(sessions []Session) []Summary {
	byKey := make(map[string]*Summary)
	order := make([]string, 0)
	for _, session := range sessions {
		key := session.Tmux.SessionID
		if key == "" {
			key = "detached:" + session.Tmux.SessionName
		}
		if key == "detached:" {
			key = "unknown"
		}

		summary, ok := byKey[key]
		if !ok {
			summary = &Summary{
				TmuxSessionID:   session.Tmux.SessionID,
				TmuxSessionName: session.Tmux.SessionName,
				Total:           0,
				Active:          0,
				Running:         0,
				Waiting:         0,
				Idle:            0,
				Unknown:         0,
				Exited:          0,
			}
			byKey[key] = summary
			order = append(order, key)
		}
		applySummaryState(summary, session.State)
	}

	summaries := make([]Summary, 0, len(order))
	for _, key := range order {
		summaries = append(summaries, *byKey[key])
	}

	return summaries
}

func (s *FileStore) GC(ctx context.Context, deleteAfter time.Duration) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, fmt.Errorf("checking context: %w", err)
	}

	now := s.now()
	result := GCResult{
		Deleted:   0,
		Remaining: 0,
	}
	err := s.withSnapshot(func(snap *snapshot) error {
		for id, session := range snap.Sessions {
			age := now.Sub(session.LastSeenAt)
			if deleteAfter > 0 && session.State == StateExited && age >= deleteAfter {
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
	result := ResetResult{
		Cleared:   0,
		Remaining: 0,
	}
	err := s.withSnapshot(func(snap *snapshot) error {
		result.Cleared = len(snap.Sessions)
		snap.Sessions = make(map[string]Session)
		snap.UpdatedAt = now
		result.Remaining = len(snap.Sessions)

		return nil
	})
	if err != nil {
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
	defer func() {
		_ = lock.Close()
	}()

	snap, err := s.load()
	if err != nil {
		return err
	}
	mutateErr := mutator(&snap)
	if mutateErr != nil {
		return mutateErr
	}

	return writeSnapshotAtomic(s.path, snap)
}

func (s *FileStore) load() (snapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newSnapshot(), nil
		}

		return snapshot{}, fmt.Errorf("reading store: %w", err)
	}

	var snap snapshot
	unmarshalErr := json.Unmarshal(data, &snap)
	if unmarshalErr != nil {
		return snapshot{}, fmt.Errorf("parsing store %s: %w", s.path, unmarshalErr)
	}
	if snap.Sessions == nil {
		snap.Sessions = make(map[string]Session)
	}
	if snap.Version == 0 {
		snap.Version = storeVersion
	}

	return snap, nil
}

func (s *FileStore) listAll() ([]Session, error) {
	snap, err := s.load()
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, 0, len(snap.Sessions))
	for _, session := range snap.Sessions {
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func newSnapshot() snapshot {
	return snapshot{
		Version:   storeVersion,
		UpdatedAt: time.Time{},
		Sessions:  make(map[string]Session),
	}
}

func writeSnapshotAtomic(path string, snap snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding store: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	mkdirErr := os.MkdirAll(dir, 0o700)
	if mkdirErr != nil {
		return fmt.Errorf("creating state directory: %w", mkdirErr)
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

	chmodErr := temp.Chmod(0o600)
	if chmodErr != nil {
		return fmt.Errorf("setting temp store permissions: %w", chmodErr)
	}
	_, writeErr := temp.Write(data)
	if writeErr != nil {
		return fmt.Errorf("writing temp store: %w", writeErr)
	}
	syncErr := temp.Sync()
	if syncErr != nil {
		return fmt.Errorf("syncing temp store: %w", syncErr)
	}
	closeErr := temp.Close()
	if closeErr != nil {
		return fmt.Errorf("closing temp store: %w", closeErr)
	}
	renameErr := os.Rename(tempPath, path)
	if renameErr != nil {
		return fmt.Errorf("renaming temp store: %w", renameErr)
	}
	keep = true

	return syncDir(dir)
}

func observedAtOrNow(observedAt time.Time, now func() time.Time) time.Time {
	if observedAt.IsZero() {
		return now().UTC()
	}

	return observedAt.UTC()
}

func applySummaryState(summary *Summary, state State) {
	summary.Total++
	if IsActive(state) {
		summary.Active++
	}

	switch state {
	case StateRunning:
		summary.Running++
	case StateWaiting:
		summary.Waiting++
	case StateIdle:
		summary.Idle++
	case StateUnknown:
		summary.Unknown++
	case StateExited:
		summary.Exited++
	default:
		summary.Unknown++
	}
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening state directory: %w", err)
	}
	defer func() {
		_ = handle.Close()
	}()

	syncErr := handle.Sync()
	if syncErr != nil {
		return fmt.Errorf("syncing state directory: %w", syncErr)
	}

	return nil
}
