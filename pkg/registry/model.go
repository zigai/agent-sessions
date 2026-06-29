package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zigai/agent-sessions/pkg/harnessmeta"
)

type Harness string

const (
	HarnessClaude   Harness = harnessmeta.IDClaude
	HarnessCodex    Harness = harnessmeta.IDCodex
	HarnessCursor   Harness = harnessmeta.IDCursor
	HarnessCopilot  Harness = harnessmeta.IDCopilot
	HarnessCline    Harness = harnessmeta.IDCline
	HarnessKimiCode Harness = harnessmeta.IDKimiCode
	HarnessGrok     Harness = harnessmeta.IDGrok
	HarnessGoose    Harness = harnessmeta.IDGoose
	HarnessPi       Harness = harnessmeta.IDPi
	HarnessOpenCode Harness = harnessmeta.IDOpenCode
	HarnessAgy      Harness = harnessmeta.IDAgy
	HarnessKilo     Harness = harnessmeta.IDKilo
	HarnessDroid    Harness = harnessmeta.IDDroid
)

var (
	// ErrUnknownHarness is returned when a harness name is not supported.
	ErrUnknownHarness = errors.New("unknown harness")
	// ErrUnknownState is returned when a lifecycle state is not supported.
	ErrUnknownState = errors.New("unknown state")
)

type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateWaiting State = "waiting"
	StateUnknown State = "unknown"
	StateExited  State = "exited"
)

type TmuxContext struct {
	Inside          bool   `json:"inside"`
	ServerSocket    string `json:"server_socket,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	SessionName     string `json:"session_name,omitempty"`
	WindowID        string `json:"window_id,omitempty"`
	WindowIndex     string `json:"window_index,omitempty"`
	WindowName      string `json:"window_name,omitempty"`
	PaneID          string `json:"pane_id,omitempty"`
	PaneIndex       string `json:"pane_index,omitempty"`
	PaneCurrentPath string `json:"pane_current_path,omitempty"`
	PanePID         int    `json:"pane_pid,omitempty"`
	PaneTTY         string `json:"pane_tty,omitempty"`
	ClientTTY       string `json:"client_tty,omitempty"`
}

func (c TmuxContext) Empty() bool {
	var empty TmuxContext

	return c == empty
}

type Session struct {
	ID               string            `json:"id"`
	Harness          Harness           `json:"harness"`
	State            State             `json:"state"`
	SessionID        string            `json:"session_id,omitempty"`
	SessionPath      string            `json:"session_path,omitempty"`
	ResumeCommand    []string          `json:"resume_command,omitempty"`
	CWD              string            `json:"cwd,omitempty"`
	ProjectRoot      string            `json:"project_root,omitempty"`
	PID              int               `json:"pid,omitempty"`
	ProcessStartTime string            `json:"process_start_time,omitempty"`
	PPID             int               `json:"ppid,omitempty"`
	TTY              string            `json:"tty,omitempty"`
	Tmux             TmuxContext       `json:"tmux,omitzero"`
	Source           string            `json:"source,omitempty"`
	Confidence       string            `json:"confidence,omitempty"`
	LastEvent        string            `json:"last_event,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"`
	RawPayload       json.RawMessage   `json:"raw_payload,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	LastSeenAt       time.Time         `json:"last_seen_at"`
	LastObservedAt   time.Time         `json:"last_observed_at,omitzero"`
	StateChangedAt   time.Time         `json:"state_changed_at,omitzero"`
	LastEventAt      time.Time         `json:"last_event_at,omitzero"`
	EndedAt          time.Time         `json:"ended_at,omitzero"`
}

type Report struct {
	Harness          Harness
	State            State
	SessionID        string
	SessionPath      string
	ResumeCommand    []string
	CWD              string
	ProjectRoot      string
	PID              int
	ProcessStartTime string
	PPID             int
	TTY              string
	Tmux             TmuxContext
	Source           string
	Confidence       string
	Event            string
	Attributes       map[string]string
	RawPayload       json.RawMessage
	ObservedAt       time.Time
}

type Filter struct {
	Harness     Harness
	State       State
	TmuxSession string
	ActiveOnly  bool
	LiveOnly    bool
}

type Summary struct {
	TmuxSessionID   string `json:"tmux_session_id,omitempty"`
	TmuxSessionName string `json:"tmux_session_name,omitempty"`
	// Total counts non-exited sessions; exited records are tracked separately.
	Total   int `json:"total"`
	Active  int `json:"active"`
	Running int `json:"running"`
	Waiting int `json:"waiting"`
	Idle    int `json:"idle"`
	Unknown int `json:"unknown"`
	Exited  int `json:"exited"`
}

type SummaryOptions struct {
	Filter Filter
}

func NormalizeHarness(value string) (Harness, error) {
	if id, ok := harnessmeta.Normalize(value); ok {
		return Harness(id), nil
	}

	return "", fmt.Errorf("%w: %q", ErrUnknownHarness, value)
}

func NormalizeState(value string) (State, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(StateIdle), "done":
		return StateIdle, nil
	case string(StateRunning), "working", "active", "busy":
		return StateRunning, nil
	case string(StateWaiting), "blocked", "needs_input", "needs-input":
		return StateWaiting, nil
	case string(StateUnknown):
		return StateUnknown, nil
	case string(StateExited), "released", "ended":
		return StateExited, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownState, value)
	}
}

func IsActive(state State) bool {
	return state == StateRunning || state == StateWaiting
}

func HasOwner(session Session) bool {
	return session.Tmux.PaneID != "" || session.PID > 0
}

func IsLive(session Session) bool {
	return session.State != StateExited && HasOwner(session)
}

func sessionIDForReport(report Report) string {
	parts := []string{string(report.Harness)}
	switch {
	case report.SessionID != "":
		parts = append(parts, "id", report.SessionID)
	case report.SessionPath != "":
		parts = append(parts, "path", filepath.Clean(report.SessionPath))
	case report.Tmux.PaneID != "":
		parts = append(parts, "tmux-pane")
		parts = append(parts, tmuxPaneIdentityParts(report.Tmux)...)
	case report.PID > 0:
		parts = append(parts, "pid", strconv.Itoa(report.PID))
	default:
		parts = append(parts, "fallback", report.CWD, report.ProjectRoot, report.TTY)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return string(report.Harness) + "-" + hex.EncodeToString(sum[:8])
}

func mergeReport(existing Session, report Report, observedAt time.Time, receivedAt time.Time, observedAtReliable bool) Session {
	session := mergeBase(existing, report, receivedAt)
	previousState := session.State
	session.Harness = report.Harness
	applyMutation := shouldApplyReportMutation(session, observedAt, observedAtReliable)

	if shouldApplyReportState(session, report, applyMutation) {
		session.State = report.State
	}
	if applyMutation {
		session = mergeLifecycle(session, previousState, report, observedAt)
		session = mergeIdentity(session, report)
		session = mergeLocation(session, report)
		session = mergeMetadata(session, report)
		if observedAtReliable {
			session.LastObservedAt = maxTime(session.LastObservedAt, observedAt)
		}
	}
	session.UpdatedAt = maxTime(session.UpdatedAt, receivedAt)
	session.LastSeenAt = maxTime(session.LastSeenAt, receivedAt)

	return session
}

func mergeBase(existing Session, report Report, receivedAt time.Time) Session {
	if existing.ID != "" {
		return existing
	}

	existing.ID = sessionIDForReport(report)
	existing.CreatedAt = receivedAt
	existing.State = StateUnknown

	return existing
}

func shouldApplyReportMutation(session Session, observedAt time.Time, observedAtReliable bool) bool {
	if !observedAtReliable {
		return false
	}

	lastObservedAt := session.LastObservedAt
	if lastObservedAt.IsZero() && session.ID == "" {
		return true
	}
	if lastObservedAt.IsZero() {
		lastObservedAt = legacyLastObservedAt(session)
	}
	if lastObservedAt.IsZero() {
		return true
	}

	return !observedAt.Before(lastObservedAt)
}

func shouldApplyReportState(session Session, report Report, applyMutation bool) bool {
	if !applyMutation {
		return false
	}
	if report.State == "" {
		return false
	}

	return report.State != StateUnknown || session.State == "" || session.State == StateUnknown
}

func mergeLifecycle(session Session, previousState State, report Report, now time.Time) Session {
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = legacyStateChangedAt(session, now)
	}
	if session.State != previousState {
		session.StateChangedAt = now
	}

	if report.Event != "" && !now.Before(session.LastEventAt) {
		session.LastEvent = report.Event
		session.LastEventAt = now
	}

	if session.State == StateExited {
		if previousState != StateExited || session.EndedAt.IsZero() {
			session.EndedAt = now
		}
	} else if previousState == StateExited {
		session.EndedAt = time.Time{}
	}

	return session
}

func legacyStateChangedAt(session Session, now time.Time) time.Time {
	switch {
	case !session.UpdatedAt.IsZero():
		return session.UpdatedAt
	case !session.CreatedAt.IsZero():
		return session.CreatedAt
	default:
		return now
	}
}

func legacyLastObservedAt(session Session) time.Time {
	return maxTime(session.LastEventAt, session.StateChangedAt)
}

func mergeIdentity(session Session, report Report) Session {
	if report.SessionID != "" {
		session.SessionID = report.SessionID
	}
	if report.SessionPath != "" {
		session.SessionPath = report.SessionPath
	}
	if len(report.ResumeCommand) > 0 {
		session.ResumeCommand = append([]string(nil), report.ResumeCommand...)
	}

	return session
}

func mergeLocation(session Session, report Report) Session {
	if report.CWD != "" {
		session.CWD = report.CWD
	}
	if report.ProjectRoot != "" {
		session.ProjectRoot = report.ProjectRoot
	}
	if report.PID > 0 {
		session.PID = report.PID
	}
	if report.ProcessStartTime != "" {
		session.ProcessStartTime = report.ProcessStartTime
	}
	if report.PPID > 0 {
		session.PPID = report.PPID
	}
	if report.TTY != "" {
		session.TTY = report.TTY
	}
	if !report.Tmux.Empty() {
		session.Tmux = report.Tmux
	}

	return session
}

func mergeMetadata(session Session, report Report) Session {
	if report.Source != "" {
		session.Source = report.Source
	}
	if report.Confidence != "" {
		session.Confidence = report.Confidence
	}
	if len(report.Attributes) > 0 {
		session.Attributes = mergeAttributes(session.Attributes, report.Attributes)
	}
	if len(report.RawPayload) > 0 {
		session.RawPayload = append(json.RawMessage(nil), report.RawPayload...)
	}

	return session
}

func mergeAttributes(existing map[string]string, incoming map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(incoming))
	maps.Copy(merged, existing)
	maps.Copy(merged, incoming)

	return merged
}

func filterSessions(sessions []Session, filter Filter) []Session {
	filtered := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if sessionMatchesFilter(session, filter) {
			filtered = append(filtered, session)
		}
	}

	sortSessions(filtered)

	return filtered
}

func sessionMatchesFilter(session Session, filter Filter) bool {
	return matchesHarnessFilter(session, filter) &&
		matchesStateFilter(session, filter) &&
		matchesActiveFilter(session, filter) &&
		matchesLiveFilter(session, filter) &&
		matchesTmuxSessionFilter(session, filter)
}

func matchesHarnessFilter(session Session, filter Filter) bool {
	return filter.Harness == "" || session.Harness == filter.Harness
}

func matchesStateFilter(session Session, filter Filter) bool {
	return filter.State == "" || session.State == filter.State
}

func matchesActiveFilter(session Session, filter Filter) bool {
	return !filter.ActiveOnly || IsActive(session.State)
}

func matchesLiveFilter(session Session, filter Filter) bool {
	return !filter.LiveOnly || IsLive(session)
}

func matchesTmuxSessionFilter(session Session, filter Filter) bool {
	return filter.TmuxSession == "" ||
		session.Tmux.SessionName == filter.TmuxSession ||
		session.Tmux.SessionID == filter.TmuxSession
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i int, j int) bool {
		left := sessions[i]
		right := sessions[j]

		if left.Tmux.SessionName != right.Tmux.SessionName {
			return left.Tmux.SessionName < right.Tmux.SessionName
		}
		if cmp := compareNumericStrings(left.Tmux.WindowIndex, right.Tmux.WindowIndex); cmp != 0 {
			return cmp < 0
		}
		if cmp := compareNumericStrings(left.Tmux.PaneIndex, right.Tmux.PaneIndex); cmp != 0 {
			return cmp < 0
		}
		if left.Harness != right.Harness {
			return left.Harness < right.Harness
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}

		return left.UpdatedAt.Before(right.UpdatedAt)
	})
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

func maxTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.After(latest) {
			latest = value
		}
	}

	return latest
}
