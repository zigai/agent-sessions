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
	"strings"
	"time"

	"github.com/zigai/agent-sessions/internal/processinfo"
)

const (
	storeVersion                    = 1
	minimumPaneReconcileSessionSize = 2
	maxObservedAtFutureSkew         = 5 * time.Minute
)

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

	report = enrichReportProcessIdentity(ctx, report)
	receivedAt := s.now().UTC()
	observedAt, observedAtReliable := observedAtOrReceivedAt(report.ObservedAt, receivedAt)
	var saved Session
	err := s.withSnapshot(func(snap *snapshot) error {
		if report.Harness == "" {
			return ErrHarnessRequired
		}
		if report.State == "" && report.SessionID == "" && report.SessionPath == "" {
			return ErrReportIdentityRequired
		}

		mutation := newReportMutation(snap, report, observedAt, receivedAt, observedAtReliable)
		saved = mutation.apply()

		return nil
	})
	if err != nil {
		return Session{}, err
	}

	return saved, nil
}

type reportMutation struct {
	snap               *snapshot
	report             Report
	observedAt         time.Time
	receivedAt         time.Time
	observedAtReliable bool
}

func newReportMutation(
	snap *snapshot,
	report Report,
	observedAt time.Time,
	receivedAt time.Time,
	observedAtReliable bool,
) reportMutation {
	return reportMutation{
		snap:               snap,
		report:             report,
		observedAt:         observedAt,
		receivedAt:         receivedAt,
		observedAtReliable: observedAtReliable,
	}
}

func (mutation *reportMutation) apply() Session {
	saved := mutation.mergeSession()
	mutation.snap.Sessions[saved.ID] = saved
	reconciledAt := mutation.reconcilePaneOccupants(saved.ID)
	saved = mutation.snap.Sessions[saved.ID]
	mutation.snap.UpdatedAt = maxTime(mutation.snap.UpdatedAt, saved.UpdatedAt, reconciledAt, mutation.receivedAt)

	return saved
}

func (mutation *reportMutation) mergeSession() Session {
	reportID := sessionIDForReport(mutation.report)
	matchingIDs := mutation.matchingSessionIDs(reportID)
	canonicalID := mutation.canonicalIDForMerge(matchingIDs, reportID)
	base := mergedExistingSessions(mutation.snap.Sessions, matchingIDs, canonicalID)
	saved := mergeReport(
		base,
		mutation.report,
		mutation.observedAt,
		mutation.receivedAt,
		mutation.observedAtReliable,
	)

	for _, id := range matchingIDs {
		delete(mutation.snap.Sessions, id)
	}

	return saved
}

func (mutation *reportMutation) canonicalIDForMerge(matchingIDs []string, reportID string) string {
	if reportHasStrongIdentity(mutation.report) {
		return reportID
	}

	for _, id := range matchingIDs {
		if sessionHasStrongIdentity(mutation.snap.Sessions[id]) {
			return id
		}
	}
	if len(matchingIDs) == 1 {
		return matchingIDs[0]
	}

	return reportID
}

// reconcilePaneOccupants enforces that one tmux pane has at most one live
// harness occupant. A later live report in the same pane means older live
// records in that pane are stale even if their harness did not emit an exit
// event.
func (mutation *reportMutation) reconcilePaneOccupants(currentID string) time.Time {
	if !mutation.reportOccupiesTmuxPane() {
		return time.Time{}
	}

	winnerID, winnerTime := mutation.currentPaneOccupant(currentID)
	if winnerID == "" {
		return time.Time{}
	}
	if winnerTime.IsZero() {
		winnerTime = mutation.observedAt
	}

	var latest time.Time
	for id, session := range mutation.snap.Sessions {
		if id == winnerID || session.State == StateExited || !sameTmuxPane(session.Tmux, mutation.report.Tmux) {
			continue
		}

		session.State = StateExited
		session.StateChangedAt = winnerTime
		session.EndedAt = winnerTime
		session.UpdatedAt = maxTime(session.UpdatedAt, winnerTime)
		session.LastSeenAt = maxTime(session.LastSeenAt, winnerTime)
		mutation.snap.Sessions[id] = session
		latest = maxTime(latest, session.UpdatedAt)
	}

	return latest
}

func (mutation *reportMutation) reportOccupiesTmuxPane() bool {
	return mutation.report.State != "" && mutation.report.State != StateExited && mutation.report.Tmux.PaneID != ""
}

func (mutation *reportMutation) currentPaneOccupant(currentID string) (string, time.Time) {
	var winnerID string
	var winnerTime time.Time
	for id, session := range mutation.snap.Sessions {
		if session.State == StateExited || !sameTmuxPane(session.Tmux, mutation.report.Tmux) {
			continue
		}

		observedAt := paneOccupantObservedAt(session)
		if paneOccupantWins(id, observedAt, currentID, winnerID, winnerTime) {
			winnerID = id
			winnerTime = observedAt
		}
	}

	return winnerID, winnerTime
}

func paneOccupantWins(
	candidateID string,
	candidateTime time.Time,
	currentID string,
	winnerID string,
	winnerTime time.Time,
) bool {
	if winnerID == "" {
		return true
	}
	if candidateTime.After(winnerTime) {
		return true
	}
	if candidateTime.Before(winnerTime) {
		return false
	}
	if candidateID == currentID && winnerID != currentID {
		return true
	}
	if winnerID == currentID {
		return false
	}

	return candidateID > winnerID
}

func paneOccupantObservedAt(session Session) time.Time {
	observedAt := session.LastObservedAt
	if observedAt.IsZero() {
		observedAt = legacyLastObservedAt(session)
	}
	if observedAt.IsZero() {
		return maxTime(session.UpdatedAt, session.LastSeenAt, session.CreatedAt)
	}

	return observedAt
}

func (mutation *reportMutation) matchingSessionIDs(canonicalID string) []string {
	matchingIDs := make([]string, 0, 1)
	for id, session := range mutation.snap.Sessions {
		if id == canonicalID || mutation.sessionMatchesReportIdentity(session) {
			matchingIDs = append(matchingIDs, id)
		}
	}
	sort.Strings(matchingIDs)
	if !reportHasStrongIdentity(mutation.report) && len(matchingIDs) > 1 {
		return []string{mutation.bestWeakIdentityMatch(matchingIDs)}
	}

	return matchingIDs
}

func (mutation *reportMutation) bestWeakIdentityMatch(ids []string) string {
	winnerID := ids[0]
	for _, id := range ids[1:] {
		if weakIdentityMatchWins(mutation.snap.Sessions[id], mutation.snap.Sessions[winnerID]) {
			winnerID = id
		}
	}

	return winnerID
}

func weakIdentityMatchWins(candidate Session, winner Session) bool {
	if sessionHasStrongIdentity(candidate) != sessionHasStrongIdentity(winner) {
		return sessionHasStrongIdentity(candidate)
	}
	if candidateAt, winnerAt := paneOccupantObservedAt(candidate), paneOccupantObservedAt(winner); !candidateAt.Equal(winnerAt) {
		return candidateAt.After(winnerAt)
	}
	if !candidate.UpdatedAt.Equal(winner.UpdatedAt) {
		return candidate.UpdatedAt.After(winner.UpdatedAt)
	}

	return candidate.ID > winner.ID
}

func (mutation *reportMutation) sessionMatchesReportIdentity(session Session) bool {
	if session.Harness != mutation.report.Harness || mutation.strongIdentityConflicts(session) {
		return false
	}
	if mutation.reportMatchesStrongSessionIdentity(session) {
		return true
	}
	if reportHasStrongIdentity(mutation.report) && sessionHasStrongIdentity(session) {
		return false
	}
	if sameTmuxPane(session.Tmux, mutation.report.Tmux) {
		return true
	}
	if mutation.report.PID > 0 && session.PID == mutation.report.PID {
		return true
	}

	return false
}

func (mutation *reportMutation) reportMatchesStrongSessionIdentity(session Session) bool {
	if mutation.report.SessionID != "" && session.SessionID == mutation.report.SessionID {
		return true
	}

	return mutation.report.SessionPath != "" && session.SessionPath != "" &&
		sameSessionPath(session.SessionPath, mutation.report.SessionPath)
}

func (mutation *reportMutation) strongIdentityConflicts(session Session) bool {
	if mutation.report.SessionID != "" && session.SessionID != "" && session.SessionID != mutation.report.SessionID {
		return true
	}
	if mutation.report.SessionPath != "" && session.SessionPath != "" &&
		!sameSessionPath(session.SessionPath, mutation.report.SessionPath) {
		return true
	}

	return false
}

func reportHasStrongIdentity(report Report) bool {
	return report.SessionID != "" || report.SessionPath != ""
}

func sessionHasStrongIdentity(session Session) bool {
	return session.SessionID != "" || session.SessionPath != ""
}

func sameSessionPath(left string, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func sameTmuxPane(left TmuxContext, right TmuxContext) bool {
	if left.PaneID == "" || right.PaneID == "" || left.PaneID != right.PaneID {
		return false
	}
	if left.ServerSocket != "" && right.ServerSocket != "" && left.ServerSocket != right.ServerSocket {
		return false
	}

	return true
}

func tmuxPaneIdentityParts(tmux TmuxContext) []string {
	var parts []string
	switch {
	case tmux.ServerSocket != "":
		parts = append(parts, "socket", filepath.Clean(tmux.ServerSocket))
	case tmux.SessionID != "":
		parts = append(parts, "session-id", tmux.SessionID)
	case tmux.SessionName != "":
		parts = append(parts, "session-name", tmux.SessionName)
	}
	parts = append(parts, "pane", tmux.PaneID)

	return parts
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
	merged.LastObservedAt = maxTime(merged.LastObservedAt, session.LastObservedAt)
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
	if session.ProcessStartTime != "" {
		merged.ProcessStartTime = session.ProcessStartTime
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

	sessions = sessionsWithReconciledPaneOccupants(sessions)

	return summariesForSessions(filterSessions(sessions, options.Filter)), nil
}

func summariesForSessions(sessions []Session) []Summary {
	sessions = sessionsWithReconciledPaneOccupants(sessions)

	byKey := make(map[string]*Summary)
	order := make([]string, 0)
	for _, session := range sessions {
		key := summaryKeyForSession(session)

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
			if deleteAfter >= 0 && session.State == StateExited && age >= deleteAfter {
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
	if err := mutator(&snap); err != nil {
		return err
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
	if err := json.Unmarshal(data, &snap); err != nil {
		return snapshot{}, fmt.Errorf("parsing store %s: %w", s.path, err)
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

func observedAtOrReceivedAt(observedAt time.Time, receivedAt time.Time) (time.Time, bool) {
	if observedAt.IsZero() {
		return receivedAt, true
	}

	observedAt = observedAt.UTC()
	if observedAt.After(receivedAt.Add(maxObservedAtFutureSkew)) {
		return receivedAt, false
	}

	return observedAt, true
}

func sessionsWithReconciledPaneOccupants(sessions []Session) []Session {
	if len(sessions) < minimumPaneReconcileSessionSize {
		return sessions
	}

	winnerByPane := make(map[string]string)
	winnerTimeByPane := make(map[string]time.Time)
	for _, session := range sessions {
		if !sessionOccupiesTmuxPane(session) {
			continue
		}

		paneID := tmuxPaneReconcileKey(session.Tmux)
		observedAt := paneOccupantObservedAt(session)
		if paneOccupantWins(session.ID, observedAt, "", winnerByPane[paneID], winnerTimeByPane[paneID]) {
			winnerByPane[paneID] = session.ID
			winnerTimeByPane[paneID] = observedAt
		}
	}
	if len(winnerByPane) == 0 {
		return sessions
	}

	reconciled := slices.Clone(sessions)
	for index, session := range reconciled {
		if !sessionOccupiesTmuxPane(session) {
			continue
		}
		if winnerByPane[tmuxPaneReconcileKey(session.Tmux)] == session.ID {
			continue
		}

		session.State = StateExited
		reconciled[index] = session
	}

	return reconciled
}

func sessionOccupiesTmuxPane(session Session) bool {
	return session.State != StateExited && session.Tmux.PaneID != ""
}

func tmuxPaneReconcileKey(tmux TmuxContext) string {
	return strings.Join(tmuxPaneIdentityParts(tmux), "\x00")
}

func enrichReportProcessIdentity(ctx context.Context, report Report) Report {
	if report.PID <= 0 || report.ProcessStartTime != "" {
		return report
	}

	report.ProcessStartTime = processinfo.StartIdentity(ctx, report.PID)

	return report
}

func summaryKeyForSession(session Session) string {
	switch {
	case session.Tmux.SessionID != "" && session.Tmux.SessionName != "":
		return "tmux:" + session.Tmux.SessionID + "\x00" + session.Tmux.SessionName
	case session.Tmux.SessionID != "":
		return "tmux:" + session.Tmux.SessionID
	case session.Tmux.SessionName != "":
		return "detached:" + session.Tmux.SessionName
	default:
		return "unknown"
	}
}

func applySummaryState(summary *Summary, state State) {
	if state != StateExited {
		summary.Total++
	}
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

	if err := handle.Sync(); err != nil {
		return fmt.Errorf("syncing state directory: %w", err)
	}

	return nil
}
