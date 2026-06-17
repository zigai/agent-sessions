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
)

type Harness string

const (
	HarnessClaude   Harness = "claude"
	HarnessCodex    Harness = "codex"
	HarnessCursor   Harness = "cursor"
	HarnessGrok     Harness = "grok"
	HarnessPi       Harness = "pi"
	HarnessOpenCode Harness = "opencode"
	HarnessAgy      Harness = "agy"
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
	StateStale   State = "stale"
)

type TmuxContext struct {
	Inside          bool   `json:"inside"`
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
	return !c.Inside &&
		c.SessionID == "" &&
		c.SessionName == "" &&
		c.WindowID == "" &&
		c.WindowIndex == "" &&
		c.WindowName == "" &&
		c.PaneID == "" &&
		c.PaneIndex == "" &&
		c.PaneCurrentPath == "" &&
		c.PanePID == 0 &&
		c.PaneTTY == "" &&
		c.ClientTTY == ""
}

type Session struct {
	ID             string            `json:"id"`
	Harness        Harness           `json:"harness"`
	State          State             `json:"state"`
	SessionID      string            `json:"session_id,omitempty"`
	SessionPath    string            `json:"session_path,omitempty"`
	ResumeCommand  []string          `json:"resume_command,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	ProjectRoot    string            `json:"project_root,omitempty"`
	PID            int               `json:"pid,omitempty"`
	PPID           int               `json:"ppid,omitempty"`
	TTY            string            `json:"tty,omitempty"`
	Tmux           TmuxContext       `json:"tmux,omitzero"`
	Source         string            `json:"source,omitempty"`
	Confidence     string            `json:"confidence,omitempty"`
	LastEvent      string            `json:"last_event,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	RawPayload     json.RawMessage   `json:"raw_payload,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastSeenAt     time.Time         `json:"last_seen_at"`
	StateChangedAt time.Time         `json:"state_changed_at,omitzero"`
	LastEventAt    time.Time         `json:"last_event_at,omitzero"`
	EndedAt        time.Time         `json:"ended_at,omitzero"`
}

type Report struct {
	Harness       Harness
	State         State
	SessionID     string
	SessionPath   string
	ResumeCommand []string
	CWD           string
	ProjectRoot   string
	PID           int
	PPID          int
	TTY           string
	Tmux          TmuxContext
	Source        string
	Confidence    string
	Event         string
	Attributes    map[string]string
	RawPayload    json.RawMessage
	ObservedAt    time.Time
}

type Filter struct {
	Harness     Harness
	State       State
	TmuxSession string
	ActiveOnly  bool
}

type Summary struct {
	TmuxSessionID   string `json:"tmux_session_id,omitempty"`
	TmuxSessionName string `json:"tmux_session_name,omitempty"`
	Total           int    `json:"total"`
	Active          int    `json:"active"`
	Running         int    `json:"running"`
	Waiting         int    `json:"waiting"`
	Idle            int    `json:"idle"`
	Unknown         int    `json:"unknown"`
	Stale           int    `json:"stale"`
	Exited          int    `json:"exited"`
}

type SummaryOptions struct {
	Filter     Filter
	StaleAfter time.Duration
	Now        time.Time
}

func NormalizeHarness(value string) (Harness, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude", "claude-code", "claude_code":
		return HarnessClaude, nil
	case string(HarnessCodex):
		return HarnessCodex, nil
	case string(HarnessCursor), "cursor-agent", "cursor_agent", "cursor-cli", "cursor_cli":
		return HarnessCursor, nil
	case string(HarnessGrok), "grok-build", "grok_build":
		return HarnessGrok, nil
	case string(HarnessPi):
		return HarnessPi, nil
	case "opencode", "open-code", "open_code":
		return HarnessOpenCode, nil
	case "agy", "antigravity", "antigravity-cli", "antigravity_cli", "google-antigravity", "google_antigravity":
		return HarnessAgy, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownHarness, value)
	}
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
	case string(StateStale):
		return StateStale, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownState, value)
	}
}

func IsActive(state State) bool {
	return state == StateRunning || state == StateWaiting
}

func sessionIDForReport(report Report) string {
	parts := []string{string(report.Harness)}
	switch {
	case report.SessionID != "":
		parts = append(parts, "id", report.SessionID)
	case report.SessionPath != "":
		parts = append(parts, "path", filepath.Clean(report.SessionPath))
	case report.Tmux.PaneID != "":
		parts = append(parts, "tmux-pane", report.Tmux.PaneID)
	case report.PID > 0:
		parts = append(parts, "pid", strconv.Itoa(report.PID))
	default:
		parts = append(parts, "fallback", report.CWD, report.ProjectRoot, report.TTY)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return string(report.Harness) + "-" + hex.EncodeToString(sum[:8])
}

func mergeReport(existing Session, report Report, now time.Time) Session {
	session := mergeBase(existing, report, now)
	previousState := session.State
	session.Harness = report.Harness

	if report.State != "" && (report.State != StateUnknown || session.State == "" || session.State == StateUnknown) {
		session.State = report.State
	}
	session = mergeLifecycle(session, previousState, report, now)

	session = mergeIdentity(session, report)
	session = mergeLocation(session, report)
	session = mergeMetadata(session, report)
	session.UpdatedAt = now
	session.LastSeenAt = now

	return session
}

func mergeBase(existing Session, report Report, now time.Time) Session {
	if existing.ID != "" {
		return existing
	}

	existing.ID = sessionIDForReport(report)
	existing.CreatedAt = now
	existing.State = StateUnknown

	return existing
}

func mergeLifecycle(session Session, previousState State, report Report, now time.Time) Session {
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = legacyStateChangedAt(session, now)
	}
	if session.State != previousState {
		session.StateChangedAt = now
	}

	if report.Event != "" {
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
		if filter.Harness != "" && session.Harness != filter.Harness {
			continue
		}
		if filter.State != "" && session.State != filter.State {
			continue
		}
		if filter.ActiveOnly && !IsActive(session.State) {
			continue
		}
		if filter.TmuxSession != "" &&
			session.Tmux.SessionName != filter.TmuxSession &&
			session.Tmux.SessionID != filter.TmuxSession {
			continue
		}
		filtered = append(filtered, session)
	}

	sortSessions(filtered)

	return filtered
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i int, j int) bool {
		left := sessions[i]
		right := sessions[j]
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
			if leftKey[index] == rightKey[index] {
				continue
			}

			return leftKey[index] < rightKey[index]
		}

		return left.UpdatedAt.Before(right.UpdatedAt)
	})
}
