package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/harnessmeta"
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
	HarnessOmp      Harness = harnessmeta.IDOmp
	HarnessOhMyPi   Harness = HarnessOmp
	HarnessOpenCode Harness = harnessmeta.IDOpenCode
	HarnessAgy      Harness = harnessmeta.IDAgy
	HarnessKilo     Harness = harnessmeta.IDKilo
	HarnessDroid    Harness = harnessmeta.IDDroid
)

var (
	ErrUnknownHarness     = errors.New("unknown harness")
	ErrUnknownPresence    = errors.New("unknown presence")
	ErrUnknownActivity    = errors.New("unknown activity")
	ErrUnknownSource      = errors.New("unknown observation source")
	ErrUnknownEvidence    = errors.New("unknown observation evidence")
	ErrInvalidObservation = errors.New("invalid observation")
)

type Presence string

const (
	PresenceLive    Presence = "live"
	PresenceGone    Presence = "gone"
	PresenceUnknown Presence = "unknown"
)

type Activity string

const (
	ActivityRunning Activity = "running"
	ActivityWaiting Activity = "waiting"
	ActivityIdle    Activity = "idle"
	ActivityUnknown Activity = "unknown"
)

type ObservationSource string

const (
	ObservationSourceNative  ObservationSource = "native"
	ObservationSourceProcess ObservationSource = "process"
	ObservationSourceTmux    ObservationSource = "tmux"
	ObservationSourceCatalog ObservationSource = "catalog"
	ObservationSourceScreen  ObservationSource = "screen"
)

type ObservationEvidence string

const (
	ObservationEvidenceNativeEvent     ObservationEvidence = "native_event"
	ObservationEvidenceProcessPresence ObservationEvidence = "process_presence"
	ObservationEvidenceTmuxLocation    ObservationEvidence = "tmux_location"
	ObservationEvidenceCatalogMetadata ObservationEvidence = "catalog_metadata"
	ObservationEvidenceScreenState     ObservationEvidence = "screen_state"
)

type NativeLifecycle string

const (
	NativeLifecycleStart  NativeLifecycle = "start"
	NativeLifecycleResume NativeLifecycle = "resume"
	NativeLifecycleEnd    NativeLifecycle = "end"
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

func (c TmuxContext) Empty() bool { return c == (TmuxContext{}) } //nolint:exhaustruct // comparing against the zero value is intentional

type ProcessIdentity struct {
	PID            int    `json:"pid"`
	PPID           int    `json:"ppid"`
	ProcessGroupID int    `json:"process_group_id"`
	Foreground     bool   `json:"foreground"`
	StartIdentity  string `json:"start_identity"`
	Executable     string `json:"executable"`
	CWD            string `json:"cwd"`
	TTY            string `json:"tty"`
}

func (p ProcessIdentity) Complete() bool { return p.PID > 0 && p.StartIdentity != "" }

func (p ProcessIdentity) Equal(other ProcessIdentity) bool {
	return p.PID == other.PID && p.StartIdentity != "" && p.StartIdentity == other.StartIdentity
}

type NativeObservation struct {
	Event                 string            `json:"event,omitempty"`
	Lifecycle             *NativeLifecycle  `json:"lifecycle,omitempty"`
	Presence              *Presence         `json:"presence,omitempty"`
	Activity              *Activity         `json:"activity,omitempty"`
	ActivityAuthoritative *bool             `json:"activity_authoritative,omitempty"`
	SessionID             string            `json:"session_id,omitempty"`
	SessionPath           string            `json:"session_path,omitempty"`
	ObservedAt            time.Time         `json:"observed_at"`
	Attributes            map[string]string `json:"attributes,omitempty"`
	RawPayload            json.RawMessage   `json:"raw_payload,omitempty"`
	Process               ProcessIdentity   `json:"process,omitzero"`
}

type ScreenObservation struct {
	Activity               Activity        `json:"activity"`
	Authority              string          `json:"authority"`
	Reason                 string          `json:"reason"`
	RuleID                 string          `json:"rule_id,omitempty"`
	ManifestSource         string          `json:"manifest_source,omitempty"`
	ManifestVersion        int             `json:"manifest_version,omitempty"`
	FallbackForIntegration string          `json:"fallback_for_integration,omitempty"`
	FallbackReason         string          `json:"fallback_reason,omitempty"`
	Process                ProcessIdentity `json:"process"`
	ObservedAt             time.Time       `json:"observed_at"`
}

type ActivityDecision struct {
	Authority       string          `json:"authority"`
	Reason          string          `json:"reason"`
	RuleID          string          `json:"rule_id,omitempty"`
	ManifestSource  string          `json:"manifest_source,omitempty"`
	ManifestVersion int             `json:"manifest_version,omitempty"`
	FallbackReason  string          `json:"fallback_reason,omitempty"`
	Process         ProcessIdentity `json:"process,omitzero"`
	ObservedAt      time.Time       `json:"observed_at"`
}

type ProcessObservation struct {
	Present    bool            `json:"present"`
	Process    ProcessIdentity `json:"process"`
	ObservedAt time.Time       `json:"observed_at"`
}

type TmuxObservation struct {
	Process    ProcessIdentity `json:"process"`
	Context    TmuxContext     `json:"context"`
	ObservedAt time.Time       `json:"observed_at"`
}

type CatalogObservation struct {
	SessionID     string    `json:"session_id,omitempty"`
	SessionPath   string    `json:"session_path,omitempty"`
	ResumeCommand []string  `json:"resume_command,omitempty"`
	CWD           string    `json:"cwd,omitempty"`
	ProjectRoot   string    `json:"project_root,omitempty"`
	ProcessPID    int       `json:"process_pid,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
}

type Observations struct {
	Native  *NativeObservation  `json:"native,omitempty"`
	Process *ProcessObservation `json:"process,omitempty"`
	Tmux    *TmuxObservation    `json:"tmux,omitempty"`
	Catalog *CatalogObservation `json:"catalog,omitempty"`
	Screen  *ScreenObservation  `json:"screen,omitempty"`
}

type Session struct {
	SchemaVersion     int               `json:"schema_version"`
	ID                string            `json:"id"`
	Harness           Harness           `json:"harness"`
	Presence          Presence          `json:"presence"`
	Activity          *Activity         `json:"activity"`
	SessionID         string            `json:"session_id,omitempty"`
	SessionPath       string            `json:"session_path,omitempty"`
	ResumeCommand     []string          `json:"resume_command,omitempty"`
	CWD               string            `json:"cwd,omitempty"`
	ProjectRoot       string            `json:"project_root,omitempty"`
	Process           *ProcessIdentity  `json:"process,omitempty"`
	Tmux              TmuxContext       `json:"tmux,omitzero"`
	Observations      Observations      `json:"observations"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	PresenceChangedAt time.Time         `json:"presence_changed_at"`
	ActivityChangedAt time.Time         `json:"activity_changed_at"`
	ActivityDecision  *ActivityDecision `json:"activity_decision,omitempty"`
}

type ObservationIdentity struct {
	SessionID   string `json:"session_id,omitempty"`
	SessionPath string `json:"session_path,omitempty"`
}

type CatalogMetadata struct {
	ResumeCommand []string `json:"resume_command,omitempty"`
	CWD           string   `json:"cwd,omitempty"`
	ProjectRoot   string   `json:"project_root,omitempty"`
	ProcessPID    int      `json:"process_pid,omitempty"`
	Current       bool     `json:"-"`
}

type Observation struct {
	Source                ObservationSource   `json:"source"`
	Evidence              ObservationEvidence `json:"evidence"`
	Harness               Harness             `json:"harness"`
	Identity              ObservationIdentity `json:"identity"`
	Lifecycle             *NativeLifecycle    `json:"lifecycle,omitempty"`
	Presence              *Presence           `json:"presence,omitempty"`
	Activity              *Activity           `json:"activity,omitempty"`
	ActivityAuthoritative *bool               `json:"activity_authoritative,omitempty"`
	NativeEvent           string              `json:"native_event,omitempty"`
	ProcessPresent        *bool               `json:"process_present,omitempty"`
	Process               *ProcessIdentity    `json:"process,omitempty"`
	Tmux                  *TmuxContext        `json:"tmux,omitempty"`
	Catalog               *CatalogMetadata    `json:"catalog,omitempty"`
	Attributes            map[string]string   `json:"attributes,omitempty"`
	RawPayload            json.RawMessage     `json:"raw_payload,omitempty"`
	Screen                *ScreenObservation  `json:"screen,omitempty"`
	ObservedAt            time.Time           `json:"observed_at"`
}

type Filter struct {
	Harness     Harness
	Presence    Presence
	Activity    Activity
	TmuxSession string
}

type Summary struct {
	TmuxSessionID   string `json:"tmux_session_id,omitempty"`
	TmuxSessionName string `json:"tmux_session_name,omitempty"`
	Total           int    `json:"total"`
	Live            int    `json:"live"`
	Gone            int    `json:"gone"`
	PresenceUnknown int    `json:"presence_unknown"`
	Running         int    `json:"running"`
	Waiting         int    `json:"waiting"`
	Idle            int    `json:"idle"`
	ActivityUnknown int    `json:"activity_unknown"`
}

type SummaryOptions struct{ Filter Filter }

func NormalizeHarness(value string) (Harness, error) {
	if id, ok := harnessmeta.Normalize(value); ok {
		return Harness(id), nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownHarness, value)
}

func NormalizePresence(value string) (Presence, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(PresenceLive):
		return PresenceLive, nil
	case string(PresenceGone):
		return PresenceGone, nil
	case string(PresenceUnknown):
		return PresenceUnknown, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownPresence, value)
	}
}

func NormalizeActivity(value string) (Activity, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(ActivityRunning):
		return ActivityRunning, nil
	case string(ActivityWaiting):
		return ActivityWaiting, nil
	case string(ActivityIdle):
		return ActivityIdle, nil
	case string(ActivityUnknown):
		return ActivityUnknown, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownActivity, value)
	}
}

func NormalizeSource(value string) (ObservationSource, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ObservationSourceNative):
		return ObservationSourceNative, nil
	case string(ObservationSourceProcess):
		return ObservationSourceProcess, nil
	case string(ObservationSourceTmux):
		return ObservationSourceTmux, nil
	case string(ObservationSourceCatalog):
		return ObservationSourceCatalog, nil
	case string(ObservationSourceScreen):
		return ObservationSourceScreen, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownSource, value)
	}
}

func NormalizeEvidence(value string) (ObservationEvidence, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ObservationEvidenceNativeEvent):
		return ObservationEvidenceNativeEvent, nil
	case string(ObservationEvidenceProcessPresence):
		return ObservationEvidenceProcessPresence, nil
	case string(ObservationEvidenceTmuxLocation):
		return ObservationEvidenceTmuxLocation, nil
	case string(ObservationEvidenceCatalogMetadata):
		return ObservationEvidenceCatalogMetadata, nil
	case string(ObservationEvidenceScreenState):
		return ObservationEvidenceScreenState, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownEvidence, value)
	}
}

func NormalizeLifecycle(value string) (NativeLifecycle, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(NativeLifecycleStart):
		return NativeLifecycleStart, nil
	case string(NativeLifecycleResume):
		return NativeLifecycleResume, nil
	case string(NativeLifecycleEnd):
		return NativeLifecycleEnd, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidObservation, value)
	}
}

//nolint:gocognit,cyclop // validation enforces source-specific evidence invariants in one place
func ValidateObservation(observation Observation) error {
	if observation.Harness == "" {
		return fmt.Errorf("%w: harness is required", ErrInvalidObservation)
	}
	if normalized, err := NormalizeHarness(string(observation.Harness)); err != nil || normalized != observation.Harness {
		return fmt.Errorf("%w: harness %q is not canonical", ErrInvalidObservation, observation.Harness)
	}
	if observation.Lifecycle != nil {
		if normalized, err := NormalizeLifecycle(string(*observation.Lifecycle)); err != nil || normalized != *observation.Lifecycle {
			return fmt.Errorf("%w: lifecycle %q is invalid", ErrInvalidObservation, *observation.Lifecycle)
		}
	}
	if observation.Presence != nil {
		if normalized, err := NormalizePresence(string(*observation.Presence)); err != nil || normalized == "" || normalized != *observation.Presence {
			return fmt.Errorf("%w: presence %q is invalid", ErrInvalidObservation, *observation.Presence)
		}
	}
	if observation.Activity != nil {
		if normalized, err := NormalizeActivity(string(*observation.Activity)); err != nil || normalized == "" || normalized != *observation.Activity {
			return fmt.Errorf("%w: activity %q is invalid", ErrInvalidObservation, *observation.Activity)
		}
	}
	if observation.Process != nil && (!observation.Process.Complete() || observation.Process.PPID < 0 || observation.Process.ProcessGroupID < 0) {
		return fmt.Errorf("%w: process identity is invalid", ErrInvalidObservation)
	}
	if observation.Tmux != nil && observation.Tmux.PanePID < 0 {
		return fmt.Errorf("%w: tmux pane pid is invalid", ErrInvalidObservation)
	}
	if observation.Catalog != nil && observation.Catalog.ProcessPID < 0 {
		return fmt.Errorf("%w: catalog process pid is invalid", ErrInvalidObservation)
	}
	if observation.Source == "" || observation.Evidence == "" {
		return fmt.Errorf("%w: source and evidence are required", ErrInvalidObservation)
	}
	if observation.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observed_at is required", ErrInvalidObservation)
	}
	if observation.Source != ObservationSourceNative && observation.Source != ObservationSourceScreen && observation.Activity != nil {
		return fmt.Errorf("%w: activity is not accepted for source %q", ErrInvalidObservation, observation.Source)
	}
	if observation.Source != ObservationSourceNative && observation.ActivityAuthoritative != nil {
		return fmt.Errorf("%w: activity authority is only accepted for native observations", ErrInvalidObservation)
	}
	pairOK := (observation.Source == ObservationSourceNative && observation.Evidence == ObservationEvidenceNativeEvent) ||
		(observation.Source == ObservationSourceProcess && observation.Evidence == ObservationEvidenceProcessPresence) ||
		(observation.Source == ObservationSourceTmux && observation.Evidence == ObservationEvidenceTmuxLocation) ||
		(observation.Source == ObservationSourceCatalog && observation.Evidence == ObservationEvidenceCatalogMetadata) ||
		(observation.Source == ObservationSourceScreen && observation.Evidence == ObservationEvidenceScreenState)
	if !pairOK {
		return fmt.Errorf("%w: source %q does not accept evidence %q", ErrInvalidObservation, observation.Source, observation.Evidence)
	}
	if observation.Source == ObservationSourceNative {
		if observation.Lifecycle != nil && *observation.Lifecycle == NativeLifecycleEnd && observation.Activity != nil {
			return fmt.Errorf("%w: end cannot include activity", ErrInvalidObservation)
		}
		if observation.Lifecycle == nil && observation.Presence == nil && observation.Activity == nil && observation.NativeEvent == "" {
			return fmt.Errorf("%w: native event or transition is required", ErrInvalidObservation)
		}
	}
	if observation.Source == ObservationSourceProcess {
		if observation.ProcessPresent == nil {
			return fmt.Errorf("%w: process_present is required", ErrInvalidObservation)
		}
		if *observation.ProcessPresent {
			if observation.Process == nil || !observation.Process.Complete() {
				return fmt.Errorf("%w: complete process identity is required", ErrInvalidObservation)
			}
		}
	}
	if observation.Source == ObservationSourceTmux {
		if observation.Tmux == nil {
			return fmt.Errorf("%w: tmux context is required", ErrInvalidObservation)
		}
		if observation.Process == nil || !observation.Process.Complete() {
			return fmt.Errorf("%w: complete process identity is required", ErrInvalidObservation)
		}
	}
	if observation.Source == ObservationSourceCatalog && observation.Catalog == nil {
		return fmt.Errorf("%w: catalog metadata is required", ErrInvalidObservation)
	}
	if observation.Source == ObservationSourceScreen {
		if observation.Activity == nil || observation.Screen == nil {
			return fmt.Errorf("%w: screen activity and evidence are required", ErrInvalidObservation)
		}
		if observation.Process == nil || !observation.Process.Complete() {
			return fmt.Errorf("%w: complete process identity is required", ErrInvalidObservation)
		}
		if !observation.Screen.Process.Equal(*observation.Process) {
			return fmt.Errorf("%w: screen process does not match observation process", ErrInvalidObservation)
		}
	}
	if observation.Identity.SessionID == "" && observation.Identity.SessionPath == "" && (observation.Process == nil || !observation.Process.Complete()) {
		return fmt.Errorf("%w: identity is required", ErrInvalidObservation)
	}
	return nil
}

func sessionIDForObservation(observation Observation) string {
	parts := []string{string(observation.Harness)}
	switch {
	case observation.Identity.SessionID != "":
		parts = append(parts, "id", observation.Identity.SessionID)
	case observation.Identity.SessionPath != "":
		parts = append(parts, "path", filepath.Clean(observation.Identity.SessionPath))
	case observation.Process != nil && observation.Process.Complete():
		parts = append(parts, "process", strconv.Itoa(observation.Process.PID), observation.Process.StartIdentity)
	default:
		parts = append(parts, "event", observation.NativeEvent)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return string(observation.Harness) + "-" + hex.EncodeToString(sum[:8])
}

func filterSessions(sessions []Session, filter Filter) []Session {
	filtered := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if filter.Harness != "" && session.Harness != filter.Harness {
			continue
		}
		if filter.Presence != "" && session.Presence != filter.Presence {
			continue
		}
		if filter.Activity != "" && (session.Activity == nil || *session.Activity != filter.Activity) {
			continue
		}
		if filter.TmuxSession != "" && session.Tmux.SessionName != filter.TmuxSession && session.Tmux.SessionID != filter.TmuxSession {
			continue
		}
		filtered = append(filtered, session)
	}
	sortSessions(filtered)
	return filtered
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i, j int) bool {
		left, right := sessions[i], sessions[j]
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

func compareNumericStrings(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr == nil && rightErr == nil {
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
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
