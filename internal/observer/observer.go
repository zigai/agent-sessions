package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/agentstate"
	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/muxctx"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

const (
	defaultObserverInterval    = 3 * time.Second
	defaultMissingSnapshots    = 2
	commandArgumentPrefixCount = 2
)

var (
	errObserverContextNil       = errors.New("observer context is nil")
	errDetectionOverrideInvalid = errors.New("agent detection override is invalid")
	errObserverCycleDegraded    = errors.New("observer cycle degraded")
	ErrAlreadyRunning           = errors.New("observer is already running")
)

type (
	ProcessLister  func(context.Context) ([]processinfo.Process, error)
	PaneLister     func(context.Context) ([]tmuxctx.Pane, error)
	ScreenCapturer func(context.Context, tmuxctx.Pane) (tmuxctx.ScreenSnapshot, error)
)

type CatalogEntry struct {
	Harness       registry.Harness `json:"harness"`
	SessionID     string           `json:"session_id,omitempty"`
	SessionPath   string           `json:"session_path,omitempty"`
	ResumeCommand []string         `json:"resume_command,omitempty"`
	CWD           string           `json:"cwd,omitempty"`
	ProjectRoot   string           `json:"project_root,omitempty"`
	ProcessPID    int              `json:"process_pid,omitempty"`
	Current       bool             `json:"current"`
}
type CatalogLister func(context.Context) ([]CatalogEntry, error)

type Options struct {
	Store                    registry.Store
	StorePath                string
	Interval                 time.Duration
	GracePeriod              time.Duration
	HealthPath               string
	ProcessList              ProcessLister
	PaneList                 PaneLister
	CatalogList              CatalogLister
	ScreenCapture            ScreenCapturer
	MultiplexerPaneList      muxctx.PaneLister
	MultiplexerScreenCapture muxctx.ScreenCapturer
	DetectionConfigDir       string
	Now                      func() time.Time
	ErrorWriter              io.Writer
	Quiet                    bool
}

type Result struct {
	ObservedAt   time.Time `json:"observed_at"`
	Observations int       `json:"observations"`
	Sessions     int       `json:"sessions"`
	Processes    int       `json:"processes"`
	Panes        int       `json:"panes"`
	Catalog      int       `json:"catalog"`
	Present      int       `json:"present"`
	Gone         int       `json:"gone"`
	Changed      int       `json:"changed"`
	Degraded     bool      `json:"degraded"`
	Error        string    `json:"error,omitempty"`
}

type Health struct {
	PID                          int           `json:"pid"`
	StartIdentity                string        `json:"start_identity,omitempty"`
	Interval                     time.Duration `json:"interval"`
	GracePeriod                  time.Duration `json:"grace_period"`
	StartedAt                    time.Time     `json:"started_at"`
	LastAttemptAt                time.Time     `json:"last_attempt_at"`
	LastSuccessAt                time.Time     `json:"last_success_at"`
	LastEnumerationErrorCategory string        `json:"last_enumeration_error_category,omitempty"`
	LastEnumerationError         string        `json:"last_enumeration_error,omitempty"`
	Cycles                       int           `json:"cycles"`
	Observations                 int           `json:"observations"`
	Sessions                     int           `json:"sessions"`
	Degraded                     bool          `json:"degraded"`
}

type processKey struct {
	harness registry.Harness
	pid     int
	start   string
}
type trackedProcess struct {
	process      processinfo.Process
	seenAt       time.Time
	missingSince time.Time
	missingCount int
}

type Observer struct {
	store          registry.Store
	storePath      string
	interval       time.Duration
	grace          time.Duration
	healthPath     string
	processList    ProcessLister
	paneList       muxctx.PaneLister
	catalogList    CatalogLister
	screenCapture  muxctx.ScreenCapturer
	tmuxOnly       bool
	manifestLoader agentstate.Loader
	now            func() time.Time
	errorWriter    io.Writer
	quiet          bool

	mu              sync.Mutex
	startedAt       time.Time
	initialized     bool
	tracked         map[processKey]trackedProcess
	health          Health
	lastHealthWrite time.Time
	lockPath        string
	lockFile        *os.File
	running         bool
}

//nolint:cyclop // constructor applies defaults for each injectable observer dependency
func New(options Options) *Observer {
	providedStorePath := options.StorePath
	storePath := options.StorePath
	store := options.Store
	if store == nil {
		if storePath == "" {
			storePath = registry.DefaultStorePath()
		}
		store = registry.NewFileStore(storePath)
	} else if providedStorePath == "" {
		storePath = ""
	}
	processList := options.ProcessList
	if processList == nil {
		processList = processinfo.List
	}
	paneList := options.MultiplexerPaneList
	tmuxOnly := false
	if paneList == nil {
		if options.PaneList != nil {
			paneList = legacyTmuxPaneLister(options.PaneList)
			tmuxOnly = true
		} else {
			paneList = listMultiplexerPanes
		}
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	interval := options.Interval
	if interval <= 0 {
		interval = defaultObserverInterval
	}
	errorWriter := options.ErrorWriter
	if errorWriter == nil {
		errorWriter = os.Stderr
	}
	healthPath := options.HealthPath
	if healthPath == "" && storePath != "" {
		healthPath = storePath + ".observer-health.json"
	}
	lockPath := ""
	if storePath != "" {
		lockPath = storePath + ".observer.lock"
	}
	catalogList := options.CatalogList
	if catalogList == nil {
		catalogList = DefaultCatalogList
	}
	screenCapture := options.MultiplexerScreenCapture
	if screenCapture == nil {
		if options.ScreenCapture != nil {
			screenCapture = legacyTmuxScreenCapturer(options.ScreenCapture)
		} else {
			screenCapture = captureMultiplexerPane
		}
	}
	return &Observer{
		store: store, storePath: storePath, interval: interval, grace: options.GracePeriod,
		healthPath: healthPath, processList: processList, paneList: paneList, catalogList: catalogList,
		screenCapture: screenCapture, tmuxOnly: tmuxOnly, manifestLoader: agentstate.Loader{ConfigDir: options.DetectionConfigDir},
		now: now, errorWriter: errorWriter, quiet: options.Quiet, tracked: make(map[processKey]trackedProcess),
		mu: sync.Mutex{}, startedAt: time.Time{}, initialized: false, health: Health{PID: 0, StartIdentity: "", Interval: 0, GracePeriod: 0, StartedAt: time.Time{}, LastAttemptAt: time.Time{}, LastSuccessAt: time.Time{}, LastEnumerationErrorCategory: "", LastEnumerationError: "", Cycles: 0, Observations: 0, Sessions: 0, Degraded: false},
		lastHealthWrite: time.Time{}, lockPath: lockPath, lockFile: nil, running: false,
	}
}

func (o *Observer) Run(ctx context.Context) error {
	return o.run(ctx, nil)
}

// RunWithResults runs the observer and calls handle after every reconciliation
// cycle. It is used by foreground clients that stream cycle results.
func (o *Observer) RunWithResults(ctx context.Context, handle func(Result) error) error {
	return o.run(ctx, handle)
}

//nolint:funcorder // shared run loop stays beside its two exported entrypoints
func (o *Observer) run(ctx context.Context, handle func(Result) error) error {
	if ctx == nil {
		return errObserverContextNil
	}
	if err := o.acquireLock(); err != nil {
		return err
	}
	defer o.releaseLock()
	for {
		result, err := o.runCycle(ctx)
		if handle != nil {
			if handleErr := handle(result); handleErr != nil {
				return fmt.Errorf("handling observer result: %w", handleErr)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if !o.quiet && o.errorWriter != nil {
				_, _ = fmt.Fprintf(o.errorWriter, "observer cycle failed: %v\n", err)
			}
		}
		timer := time.NewTimer(o.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (o *Observer) RunOnce(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, errObserverContextNil
	}
	if err := o.acquireLock(); err != nil {
		return Result{}, err
	}
	defer o.releaseLock()
	return o.runCycle(ctx)
}

//nolint:gocognit,cyclop,funcorder,maintidx // one cycle correlates process, multiplexer, catalog, and terminal evidence
func (o *Observer) runCycle(ctx context.Context) (Result, error) {
	at := o.now().UTC()
	result := Result{ObservedAt: at, Observations: 0, Sessions: 0, Processes: 0, Panes: 0, Catalog: 0, Present: 0, Gone: 0, Changed: 0, Degraded: false, Error: ""}
	if err := o.initializeTracked(ctx); err != nil {
		result.Degraded = true
		result.Error = err.Error()
		healthErr := o.recordHealth(at, true, "registry", err, result)
		return result, joinObserverHealthError(fmt.Errorf("initializing observer state: %w", err), healthErr)
	}
	processes, err := o.processList(ctx)
	if err != nil {
		result.Degraded = true
		result.Error = err.Error()
		healthErr := o.recordHealth(at, true, "process-enumeration", err, result)
		return result, joinObserverHealthError(fmt.Errorf("listing processes: %w", err), healthErr)
	}
	result.Processes = len(processes)
	panes, paneErr := o.paneList(ctx)
	if paneErr != nil {
		result.Degraded = true
		result.Error = paneErr.Error()
	}
	result.Panes = len(panes)
	sort.SliceStable(panes, func(left, right int) bool {
		return multiplexerPriority(panes[left].Location.Kind) > multiplexerPriority(panes[right].Location.Kind)
	})
	paneCommandCounts := commandPaneCounts(panes)
	catalog, catalogErr := o.listCatalog(ctx)
	if catalogErr != nil {
		result.Degraded = true
		result.Error = catalogErr.Error()
	}
	result.Catalog = len(catalog)
	knownSessions, sessionErr := o.store.List(ctx, registry.Filter{Harness: "", Presence: "", Activity: "", TmuxSession: "", MultiplexerSession: ""})
	if sessionErr != nil {
		result.Degraded = true
		result.Error = sessionErr.Error()
		healthErr := o.recordHealth(at, true, "registry", sessionErr, result)
		return result, joinObserverHealthError(fmt.Errorf("listing sessions for state detection: %w", sessionErr), healthErr)
	}

	catalogByPID := make(map[int]CatalogEntry)
	for _, entry := range catalog {
		if entry.Current && entry.ProcessPID > 0 {
			catalogByPID[entry.ProcessPID] = entry
		}
	}
	observations := make([]registry.Observation, 0, len(processes)+len(panes)+len(catalog))
	current := make(map[processKey]trackedProcess)
	processByPID := make(map[int]processinfo.Process, len(processes))
	harnessByPID := make(map[int]registry.Harness, len(processes))
	for _, process := range processes {
		if process.PID <= 0 || process.StartIdentity == "" {
			continue
		}
		harnessID, ok := resolveHarness(process)
		if !ok {
			continue
		}
		key := processKey{harness: harnessID, pid: process.PID, start: process.StartIdentity}
		current[key] = trackedProcess{process: process, seenAt: at, missingSince: time.Time{}, missingCount: 0}
		processByPID[process.PID] = process
		harnessByPID[process.PID] = harnessID
		present := true
		identity := registry.ObservationIdentity{SessionID: "", SessionPath: ""}
		if entry, ok := catalogByPID[process.PID]; ok && entry.Harness == harnessID {
			identity = registry.ObservationIdentity{SessionID: entry.SessionID, SessionPath: entry.SessionPath}
		}
		observations = append(observations, registry.Observation{ //nolint:exhaustruct // process observations intentionally omit unrelated evidence dimensions
			Source: registry.ObservationSourceProcess, Evidence: registry.ObservationEvidenceProcessPresence,
			Harness: harnessID, Identity: identity, ProcessPresent: &present, Process: processIdentity(process), ObservedAt: at,
		})
		result.Present++
	}
	locationPIDs := make(map[int]bool)
	for _, pane := range panes {
		process, harnessID, ok := multiplexerPaneProcess(pane, processes, processByPID, harnessByPID, paneCommandCounts)
		if !ok {
			continue
		}
		if locationPIDs[process.PID] {
			continue
		}
		location := pane.Location
		if location.PanePID == 0 && len(pane.Processes) > 0 {
			location.PanePID = pane.Processes[0].PID
		}
		if location.PaneTTY == "" {
			location.PaneTTY = pane.ProcessTTY
		}
		if location.Kind == registry.MultiplexerTmux {
			tmuxContext := location.TmuxContext()
			observations = append(observations, registry.Observation{ //nolint:exhaustruct // tmux observations intentionally omit unrelated evidence dimensions
				Source: registry.ObservationSourceTmux, Evidence: registry.ObservationEvidenceTmuxLocation,
				Harness: harnessID, Process: processIdentity(process), Tmux: &tmuxContext, ObservedAt: at,
			})
		} else {
			observations = append(observations, registry.Observation{ //nolint:exhaustruct // multiplexer observations intentionally omit unrelated evidence dimensions
				Source: registry.ObservationSourceMultiplexer, Evidence: registry.ObservationEvidenceMultiplexerLocation,
				Harness: harnessID, Process: processIdentity(process), Multiplexer: &location, ObservedAt: at,
			})
		}
		if screenObservation, detected, detectErr := o.detectScreenState(ctx, knownSessions, harnessID, process, pane, at); detected {
			observations = append(observations, screenObservation)
			if detectErr != nil {
				result.Degraded = true
				result.Error = detectErr.Error()
			}
		}
		locationPIDs[process.PID] = true
	}
	observations = append(observations, observationsForUnlocatedProcesses(knownSessions, processByPID, harnessByPID, locationPIDs, paneErr, o.tmuxOnly, at)...)
	for _, entry := range catalog {
		if entry.Harness == "" || entry.SessionID == "" {
			continue
		}
		metadata := &registry.CatalogMetadata{ResumeCommand: append([]string(nil), entry.ResumeCommand...), CWD: entry.CWD, ProjectRoot: entry.ProjectRoot, ProcessPID: entry.ProcessPID, Current: entry.Current}
		observations = append(observations, registry.Observation{ //nolint:exhaustruct // catalog observations intentionally omit unrelated evidence dimensions
			Source: registry.ObservationSourceCatalog, Evidence: registry.ObservationEvidenceCatalogMetadata,
			Harness: entry.Harness, Identity: registry.ObservationIdentity{SessionID: entry.SessionID, SessionPath: entry.SessionPath},
			Catalog: metadata, ObservedAt: at,
		})
	}
	nextTracked := current
	if o.initialized {
		nextTracked = make(map[processKey]trackedProcess, len(current)+len(o.tracked))
		maps.Copy(nextTracked, current)
		for key, old := range o.tracked {
			if _, ok := current[key]; ok {
				continue
			}
			old.missingCount++
			if old.missingSince.IsZero() {
				old.missingSince = at
			}
			eligible := o.grace > 0 && at.Sub(old.missingSince) >= o.grace
			if o.grace == 0 {
				eligible = old.missingCount >= defaultMissingSnapshots
			}
			if eligible {
				present := false
				observations = append(observations, registry.Observation{ //nolint:exhaustruct // absence observations intentionally omit unrelated evidence dimensions
					Source: registry.ObservationSourceProcess, Evidence: registry.ObservationEvidenceProcessPresence,
					Harness: key.harness, ProcessPresent: &present, Process: processIdentity(old.process), ObservedAt: at,
				})
				result.Gone++
				continue
			}
			nextTracked[key] = old
		}
	}
	if len(observations) > 0 {
		sessions, observeErr := o.store.ObserveBatch(ctx, observations)
		if observeErr != nil {
			healthErr := o.recordHealth(at, true, "registry", observeErr, result)
			return result, joinObserverHealthError(fmt.Errorf("recording observations: %w", observeErr), healthErr)
		}
		result.Sessions = len(sessions)
		result.Changed = len(sessions)
	}
	o.tracked = nextTracked
	o.initialized = true
	result.Observations = len(observations)
	if err := o.recordCycleHealth(at, result); err != nil {
		result.Degraded = true
		result.Error = err.Error()
		return result, fmt.Errorf("recording observer health: %w", err)
	}
	return result, nil
}

//nolint:funcorder // cycle health handling stays next to reconciliation
func (o *Observer) recordCycleHealth(at time.Time, result Result) error {
	if result.Degraded {
		return o.recordHealth(at, true, "reconciliation", fmt.Errorf("%w: %s", errObserverCycleDegraded, result.Error), result)
	}
	return o.recordHealth(at, false, "", nil, result)
}

func joinObserverHealthError(primary, healthErr error) error {
	if healthErr == nil {
		return primary
	}

	return errors.Join(primary, fmt.Errorf("recording observer health: %w", healthErr))
}

func resolveHarness(process processinfo.Process) (registry.Harness, bool) {
	if process.AgentHint != "" {
		if harnessID, err := registry.NormalizeHarness(process.AgentHint); err == nil {
			return harnessID, true
		}
	}
	if harnessID, ok := harness.FromCommand(process.Executable); ok {
		return harnessID, true
	}
	for _, arg := range process.Args[:min(commandArgumentPrefixCount, len(process.Args))] {
		if harnessID, ok := harness.FromCommand(arg); ok {
			return harnessID, true
		}
	}
	if isAgentWrapper(process) {
		start := min(commandArgumentPrefixCount, len(process.Args))
		for _, arg := range process.Args[start:] {
			if harnessID, ok := harness.FromCommand(arg); ok {
				return harnessID, true
			}
		}
	}
	return "", false
}

func isAgentWrapper(process processinfo.Process) bool {
	command := filepath.Base(process.Executable)
	if command == "" && len(process.Args) > 0 {
		command = filepath.Base(process.Args[0])
	}
	switch command {
	case "env", "fence", "bwrap", "bubblewrap", "mise", "nix-shell", "nix", "direnv":
		return true
	default:
		return false
	}
}

func observationsForUnlocatedProcesses(sessions []registry.Session, processByPID map[int]processinfo.Process, harnessByPID map[int]registry.Harness, locationPIDs map[int]bool, paneErr error, tmuxOnly bool, at time.Time) []registry.Observation {
	observations := make([]registry.Observation, 0, len(processByPID))
	for pid, process := range processByPID {
		if locationPIDs[pid] {
			continue
		}
		harnessID := harnessByPID[pid]
		unavailableReason := "screen_not_in_supported_multiplexer"
		if tmuxOnly {
			unavailableReason = "screen_not_in_tmux"
		}
		if paneErr == nil {
			if tmuxOnly {
				emptyContext := registry.TmuxContext{}                    //nolint:exhaustruct // zero-value tmux context represents no pane location
				observations = append(observations, registry.Observation{ //nolint:exhaustruct // tmux observations intentionally omit unrelated evidence dimensions
					Source: registry.ObservationSourceTmux, Evidence: registry.ObservationEvidenceTmuxLocation,
					Harness: harnessID, Process: processIdentity(process), Tmux: &emptyContext, ObservedAt: at,
				})
			} else {
				emptyContext := registry.MultiplexerContext{}             //nolint:exhaustruct // zero-value context represents no supported multiplexer pane
				observations = append(observations, registry.Observation{ //nolint:exhaustruct // location observations intentionally omit unrelated evidence dimensions
					Source: registry.ObservationSourceMultiplexer, Evidence: registry.ObservationEvidenceMultiplexerLocation,
					Harness: harnessID, Process: processIdentity(process), Multiplexer: &emptyContext, ObservedAt: at,
				})
			}
		} else {
			unavailableReason = "multiplexer_enumeration_failed"
			if tmuxOnly {
				unavailableReason = "tmux_enumeration_failed"
			}
		}
		if screenObservation, detected := unavailableScreenState(sessions, harnessID, process, at, unavailableReason); detected {
			observations = append(observations, screenObservation)
		}
	}
	return observations
}

func sessionForProcess(sessions []registry.Session, harnessID registry.Harness, identity *registry.ProcessIdentity) registry.Session {
	session := registry.Session{ //nolint:exhaustruct // synthetic session only carries policy inputs
		Harness: harnessID,
		Process: identity,
	}
	for _, candidate := range sessions {
		if candidate.Harness == harnessID && candidate.Process != nil && candidate.Process.Equal(*identity) {
			return candidate
		}
	}
	return session
}

func screenFallbackMetadata(session registry.Session, harnessID registry.Harness, at time.Time) (string, string) {
	policy := agentstate.PolicyFor(harnessID)
	if policy.Primary != agentstate.AuthorityHook {
		return "", ""
	}
	return policy.IntegrationValue, agentstate.EvaluateHook(session, at).Reason
}

func unavailableScreenState(sessions []registry.Session, harnessID registry.Harness, process processinfo.Process, at time.Time, reason string) (registry.Observation, bool) {
	if !agentstate.SupportsScreen(harnessID) {
		var empty registry.Observation
		return empty, false
	}
	identity := processIdentity(process)
	session := sessionForProcess(sessions, harnessID, identity)
	if !agentstate.ShouldDetectScreen(session, at) {
		var empty registry.Observation
		return empty, false
	}
	fallback, fallbackReason := screenFallbackMetadata(session, harnessID, at)
	unknown := registry.ActivityUnknown
	screen := &registry.ScreenObservation{Activity: unknown, Authority: string(agentstate.AuthorityScreen), Reason: reason, RuleID: "", ManifestSource: "", ManifestVersion: 0, FallbackForIntegration: fallback, FallbackReason: fallbackReason, Process: *identity, ObservedAt: at}
	observation := registry.Observation{ //nolint:exhaustruct // unavailable evidence intentionally has no terminal data
		Source: registry.ObservationSourceScreen, Evidence: registry.ObservationEvidenceScreenState, Harness: harnessID,
		Activity: &unknown, Process: identity, Screen: screen, ObservedAt: at,
	}
	return observation, true
}

//nolint:funcorder // state detection runs as part of reconciliation near its call site
func (o *Observer) detectScreenState(ctx context.Context, sessions []registry.Session, harnessID registry.Harness, process processinfo.Process, pane muxctx.Pane, at time.Time) (registry.Observation, bool, error) {
	if !agentstate.SupportsScreen(harnessID) {
		var empty registry.Observation
		return empty, false, nil
	}
	identity := processIdentity(process)
	session := sessionForProcess(sessions, harnessID, identity)
	if !agentstate.ShouldDetectScreen(session, at) {
		var empty registry.Observation
		return empty, false, nil
	}
	if pane.Activity != nil {
		fallback, fallbackReason := screenFallbackMetadata(session, harnessID, at)
		reason := pane.StateReason
		if reason == "" {
			reason = "multiplexer_agent_status"
		}
		screen := &registry.ScreenObservation{
			Activity: *pane.Activity, Authority: string(pane.Location.Kind), Reason: reason,
			RuleID: "", ManifestSource: "", ManifestVersion: 0,
			FallbackForIntegration: fallback, FallbackReason: fallbackReason,
			Process: *identity, ObservedAt: at,
		}
		observation := registry.Observation{ //nolint:exhaustruct // semantic multiplexer state contains no terminal contents
			Source: registry.ObservationSourceScreen, Evidence: registry.ObservationEvidenceScreenState, Harness: harnessID,
			Activity: pane.Activity, Process: identity, Screen: screen, ObservedAt: at,
		}
		return observation, true, nil
	}
	manifest, err := o.manifestLoader.Load(harnessID)
	if err != nil {
		return registry.Observation{}, false, fmt.Errorf("loading %s detection manifest: %w", harnessID, err)
	}
	decision := unavailableScreenDecision(manifest)
	snapshot, captureErr := o.screenCapture(ctx, pane)
	if captureErr == nil {
		decision = agentstate.Evaluate(manifest, agentstate.NormalizeSnapshot(snapshot.Text, snapshot.Title))
	}
	observedAt := screenObservationTime(at, o.now().UTC())
	fallback, fallbackReason := screenFallbackMetadata(session, harnessID, at)
	screen := &registry.ScreenObservation{Activity: decision.Activity, Authority: string(agentstate.AuthorityScreen), Reason: decision.Reason, RuleID: decision.RuleID, ManifestSource: decision.ManifestSource, ManifestVersion: decision.ManifestVersion, FallbackForIntegration: fallback, FallbackReason: fallbackReason, Process: *identity, ObservedAt: observedAt}
	observation := registry.Observation{ //nolint:exhaustruct // screen observations intentionally contain no terminal contents
		Source: registry.ObservationSourceScreen, Evidence: registry.ObservationEvidenceScreenState, Harness: harnessID,
		Activity: &decision.Activity, Process: identity, Screen: screen, ObservedAt: observedAt,
	}
	if captureErr != nil {
		return observation, true, fmt.Errorf("capturing %s pane %s for detection: %w", harnessID, pane.Location.PaneID, captureErr)
	}
	if manifest.Warning != "" {
		return observation, true, fmt.Errorf("%w: %s", errDetectionOverrideInvalid, manifest.Warning)
	}
	return observation, true, nil
}

func screenObservationTime(cycleAt time.Time, capturedAt time.Time) time.Time {
	if capturedAt.Before(cycleAt) {
		return cycleAt
	}
	return capturedAt
}

func unavailableScreenDecision(manifest agentstate.Manifest) agentstate.Decision {
	return agentstate.Decision{Activity: registry.ActivityUnknown, Reason: "screen_unavailable", RuleID: "", ManifestSource: manifest.Source, ManifestVersion: manifest.Version, Warning: manifest.Warning, Evidence: nil}
}

func paneProcess(pane tmuxctx.Pane, processes []processinfo.Process, byPID map[int]processinfo.Process, harnessByPID map[int]registry.Harness) (processinfo.Process, registry.Harness, bool) {
	converted := multiplexerPanesFromTmux([]tmuxctx.Pane{pane})
	return multiplexerPaneProcess(converted[0], processes, byPID, harnessByPID, commandPaneCounts(converted))
}

func preferForegroundProcess(candidate processinfo.Process, current processinfo.Process) bool {
	candidateDirect := isDirectAgentProcess(candidate)
	currentDirect := isDirectAgentProcess(current)
	if candidateDirect != currentDirect {
		return candidateDirect
	}
	candidateLeader := candidate.PID == candidate.ProcessGroupID
	currentLeader := current.PID == current.ProcessGroupID
	if candidateLeader != currentLeader {
		return candidateLeader
	}
	return candidate.PID < current.PID
}

func isDirectAgentProcess(process processinfo.Process) bool {
	if isAgentWrapper(process) {
		return false
	}
	if _, ok := harness.FromCommand(process.Executable); ok {
		return true
	}
	for _, arg := range process.Args[:min(commandArgumentPrefixCount, len(process.Args))] {
		if _, ok := harness.FromCommand(arg); ok {
			return true
		}
	}
	return false
}

func processIdentity(process processinfo.Process) *registry.ProcessIdentity {
	return &registry.ProcessIdentity{PID: process.PID, PPID: process.PPID, ProcessGroupID: process.ProcessGroupID, Foreground: process.Foreground, StartIdentity: process.StartIdentity, Executable: process.Executable, CWD: process.CWD, TTY: process.TTY}
}

//nolint:funcorder // private helpers are grouped with lock and reconciliation internals
func (o *Observer) initializeTracked(ctx context.Context) error {
	if o.initialized {
		return nil
	}
	sessions, err := o.store.List(ctx, registry.Filter{Harness: "", Presence: registry.PresenceLive, Activity: "", TmuxSession: "", MultiplexerSession: ""})
	if err != nil {
		return fmt.Errorf("listing live sessions: %w", err)
	}
	for _, session := range sessions {
		observation := session.Observations.Process
		if observation == nil || !observation.Present || !observation.Process.Complete() {
			continue
		}
		process := processinfo.Process{
			PID:             observation.Process.PID,
			PPID:            observation.Process.PPID,
			ProcessGroupID:  observation.Process.ProcessGroupID,
			Foreground:      observation.Process.Foreground,
			StartIdentity:   observation.Process.StartIdentity,
			Executable:      observation.Process.Executable,
			CWD:             observation.Process.CWD,
			TTY:             observation.Process.TTY,
			AgentHint:       "",
			MultiplexerKind: "", MultiplexerServer: "", MultiplexerSession: "", MultiplexerPane: "",
			Args: nil,
		}
		key := processKey{harness: session.Harness, pid: process.PID, start: process.StartIdentity}
		o.tracked[key] = trackedProcess{
			process:      process,
			seenAt:       observation.ObservedAt,
			missingSince: observation.ObservedAt,
			missingCount: defaultMissingSnapshots - 1,
		}
	}
	o.initialized = true
	return nil
}

//nolint:funcorder // private helpers are grouped with lock and reconciliation internals
func (o *Observer) listCatalog(ctx context.Context) ([]CatalogEntry, error) {
	if o.catalogList == nil {
		return nil, nil
	}
	return o.catalogList(ctx)
}

//nolint:funcorder // private helpers are grouped with lock and reconciliation internals
func (o *Observer) acquireLock() error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return ErrAlreadyRunning
	}
	o.running = true
	o.mu.Unlock()

	if o.lockPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(o.lockPath), 0o700); err != nil {
		o.clearRunning()
		return fmt.Errorf("create observer lock directory: %w", err)
	}
	file, err := openObserverLock(o.lockPath)
	if err != nil {
		o.clearRunning()
		return fmt.Errorf("observer already running or lock unavailable: %w", err)
	}
	o.mu.Lock()
	o.lockFile = file
	o.mu.Unlock()
	return nil
}

//nolint:funcorder // private helpers are grouped with lock and reconciliation internals
func (o *Observer) releaseLock() {
	o.mu.Lock()
	file := o.lockFile
	o.lockFile = nil
	o.running = false
	o.mu.Unlock()
	if file == nil {
		return
	}
	_ = closeObserverLock(file)
	_ = removeObserverLock(o.lockPath)
}

//nolint:cyclop,funcorder // health persistence handles degraded categories and atomic writes
func (o *Observer) recordHealth(at time.Time, degraded bool, category string, err error, result Result) error {
	o.mu.Lock()
	wasDegraded := o.health.Degraded
	if o.startedAt.IsZero() {
		o.startedAt = at
		o.health.PID = os.Getpid()
		o.health.StartedAt = at
		o.health.Interval = o.interval
		o.health.GracePeriod = o.grace
	}
	o.health.LastAttemptAt = at
	o.health.Cycles++
	o.health.Observations += result.Observations
	o.health.Sessions += result.Sessions
	o.health.Degraded = degraded
	if err != nil {
		o.health.LastEnumerationErrorCategory = category
		o.health.LastEnumerationError = err.Error()
	} else if !degraded {
		o.health.LastSuccessAt = at
		o.health.LastEnumerationErrorCategory = ""
		o.health.LastEnumerationError = ""
	}
	health := o.health
	shouldWrite := o.lastHealthWrite.IsZero() || at.Sub(o.lastHealthWrite) >= 30*time.Second || err != nil || degraded != wasDegraded
	o.mu.Unlock()
	if !shouldWrite || o.healthPath == "" {
		return nil
	}
	data, marshalErr := json.MarshalIndent(health, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("encoding observer health: %w", marshalErr)
	}
	if err := os.MkdirAll(filepath.Dir(o.healthPath), 0o700); err != nil {
		return fmt.Errorf("creating observer health directory: %w", err)
	}
	tmp := o.healthPath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing observer health: %w", err)
	}
	if err := os.Rename(tmp, o.healthPath); err != nil {
		cleanupErr := os.Remove(tmp)
		if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			cleanupErr = fmt.Errorf("removing temporary observer health file: %w", cleanupErr)
		} else {
			cleanupErr = nil
		}

		return errors.Join(fmt.Errorf("publishing observer health: %w", err), cleanupErr)
	}
	o.mu.Lock()
	o.lastHealthWrite = at
	o.mu.Unlock()

	return nil
}

func (o *Observer) Health() Health {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.health
}

func (o *Observer) clearRunning() {
	o.mu.Lock()
	o.running = false
	o.mu.Unlock()
}

func (r Result) String() string {
	return fmt.Sprintf("observations=%d sessions=%d processes=%d panes=%d", r.Observations, r.Sessions, r.Processes, r.Panes)
}
