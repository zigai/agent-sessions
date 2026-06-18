package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

		id := sessionIDForReport(report)
		saved = mergeReport(snap.Sessions[id], report, now)
		snap.Sessions[saved.ID] = saved
		snap.UpdatedAt = now

		return nil
	})
	if err != nil {
		return Session{}, err
	}

	return saved, nil
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
