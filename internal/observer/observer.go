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
	"sync"
	"time"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

const (
	defaultObserverInterval    = 3 * time.Second
	defaultMissingSnapshots    = 2
	commandArgumentPrefixCount = 2
)

var errObserverContextNil = errors.New("observer context is nil")

type (
	ProcessLister func(context.Context) ([]processinfo.Process, error)
	PaneLister    func(context.Context) ([]tmuxctx.Pane, error)
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
	Store       registry.Store
	StorePath   string
	Interval    time.Duration
	GracePeriod time.Duration
	HealthPath  string
	ProcessList ProcessLister
	PaneList    PaneLister
	CatalogList CatalogLister
	Now         func() time.Time
	ErrorWriter io.Writer
	Quiet       bool
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
	goneReported bool
}

type Observer struct {
	store       registry.Store
	storePath   string
	interval    time.Duration
	grace       time.Duration
	healthPath  string
	processList ProcessLister
	paneList    PaneLister
	catalogList CatalogLister
	now         func() time.Time
	errorWriter io.Writer
	quiet       bool

	mu              sync.Mutex
	startedAt       time.Time
	initialized     bool
	tracked         map[processKey]trackedProcess
	health          Health
	lastHealthWrite time.Time
	lockPath        string
	lockFile        *os.File
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
	paneList := options.PaneList
	if paneList == nil {
		paneList = tmuxctx.ListPanes
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
	return &Observer{
		store: store, storePath: storePath, interval: interval, grace: options.GracePeriod,
		healthPath: healthPath, processList: processList, paneList: paneList, catalogList: catalogList,
		now: now, errorWriter: errorWriter, quiet: options.Quiet, tracked: make(map[processKey]trackedProcess),
		mu: sync.Mutex{}, startedAt: time.Time{}, initialized: false, health: Health{PID: 0, StartIdentity: "", Interval: 0, GracePeriod: 0, StartedAt: time.Time{}, LastAttemptAt: time.Time{}, LastSuccessAt: time.Time{}, LastEnumerationErrorCategory: "", LastEnumerationError: "", Cycles: 0, Observations: 0, Sessions: 0, Degraded: false},
		lastHealthWrite: time.Time{}, lockPath: lockPath, lockFile: nil,
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

//nolint:gocognit,cyclop,funcorder // one reconciliation cycle correlates process, tmux, catalog, and terminal evidence
func (o *Observer) runCycle(ctx context.Context) (Result, error) {
	at := o.now().UTC()
	result := Result{ObservedAt: at, Observations: 0, Sessions: 0, Processes: 0, Panes: 0, Catalog: 0, Present: 0, Gone: 0, Changed: 0, Degraded: false, Error: ""}
	processes, err := o.processList(ctx)
	if err != nil {
		result.Degraded = true
		result.Error = err.Error()
		o.recordHealth(at, true, "process-enumeration", err, result)
		return result, fmt.Errorf("listing processes: %w", err)
	}
	result.Processes = len(processes)
	panes, paneErr := o.paneList(ctx)
	if paneErr != nil {
		result.Degraded = true
		result.Error = paneErr.Error()
	}
	result.Panes = len(panes)
	catalog, catalogErr := o.listCatalog(ctx)
	if catalogErr != nil {
		result.Degraded = true
		result.Error = catalogErr.Error()
	}
	result.Catalog = len(catalog)

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
		current[key] = trackedProcess{process: process, seenAt: at, missingSince: time.Time{}, missingCount: 0, goneReported: false}
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
		process, harnessID, ok := paneProcess(pane, processes, processByPID, harnessByPID)
		if !ok {
			continue
		}
		context := pane.Tmux
		context.PanePID = pane.PanePID
		context.PaneTTY = pane.PaneTTY
		observations = append(observations, registry.Observation{ //nolint:exhaustruct // tmux observations intentionally omit unrelated evidence dimensions
			Source: registry.ObservationSourceTmux, Evidence: registry.ObservationEvidenceTmuxLocation,
			Harness: harnessID, Process: processIdentity(process), Tmux: &context, ObservedAt: at,
		})
		locationPIDs[process.PID] = true
	}
	if paneErr == nil {
		for pid, process := range processByPID {
			if locationPIDs[pid] {
				continue
			}
			harnessID := harnessByPID[pid]
			emptyContext := registry.TmuxContext{}                    //nolint:exhaustruct // zero-value tmux context represents no pane location
			observations = append(observations, registry.Observation{ //nolint:exhaustruct // tmux observations intentionally omit unrelated evidence dimensions
				Source: registry.ObservationSourceTmux, Evidence: registry.ObservationEvidenceTmuxLocation,
				Harness: harnessID, Process: processIdentity(process), Tmux: &emptyContext, ObservedAt: at,
			})
		}
	}
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
	if o.initialized {
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
			if eligible && !old.goneReported {
				present := false
				observations = append(observations, registry.Observation{ //nolint:exhaustruct // absence observations intentionally omit unrelated evidence dimensions
					Source: registry.ObservationSourceProcess, Evidence: registry.ObservationEvidenceProcessPresence,
					Harness: key.harness, ProcessPresent: &present, Process: processIdentity(old.process), ObservedAt: at,
				})
				old.goneReported = true
				result.Gone++
			}
			o.tracked[key] = old
		}
	}
	if len(observations) > 0 {
		sessions, observeErr := o.store.ObserveBatch(ctx, observations)
		if observeErr != nil {
			o.recordHealth(at, true, "registry", observeErr, result)
			return result, fmt.Errorf("recording observations: %w", observeErr)
		}
		result.Sessions = len(sessions)
		result.Changed = len(sessions)
	}
	o.mu.Lock()
	maps.Copy(o.tracked, current)
	o.initialized = true
	o.mu.Unlock()
	result.Observations = len(observations)
	o.recordHealth(at, result.Degraded, "", nil, result)
	return result, nil
}

func resolveHarness(process processinfo.Process) (registry.Harness, bool) {
	if harnessID, ok := harness.FromCommand(process.Executable); ok {
		return harnessID, true
	}
	for _, arg := range process.Args[:min(commandArgumentPrefixCount, len(process.Args))] {
		if harnessID, ok := harness.FromCommand(arg); ok {
			return harnessID, true
		}
	}
	return "", false
}

//nolint:gocognit,cyclop // pane correlation follows direct, tty, and ancestor matching paths
func paneProcess(pane tmuxctx.Pane, processes []processinfo.Process, byPID map[int]processinfo.Process, harnessByPID map[int]registry.Harness) (processinfo.Process, registry.Harness, bool) {
	if process, ok := byPID[pane.PanePID]; ok {
		if harnessID, ok := harnessByPID[pane.PanePID]; ok {
			return process, harnessID, true
		}
	}
	for _, process := range processes {
		if process.PID <= 0 {
			continue
		}
		ancestor := process
		for range processes {
			if ancestor.PPID == pane.PanePID {
				if harnessID, ok := harnessByPID[ancestor.PID]; ok {
					return ancestor, harnessID, true
				}
			}
			next, ok := byPID[ancestor.PPID]
			if !ok || next.PID == ancestor.PID {
				break
			}
			ancestor = next
		}
		if pane.PaneTTY != "" && process.TTY == pane.PaneTTY {
			if harnessID, ok := harnessByPID[process.PID]; ok {
				return process, harnessID, true
			}
		}
	}
	return processinfo.Process{PID: 0, PPID: 0, ProcessGroupID: 0, StartIdentity: "", Executable: "", CWD: "", TTY: "", Args: nil}, "", false
}

func processIdentity(process processinfo.Process) *registry.ProcessIdentity {
	return &registry.ProcessIdentity{PID: process.PID, PPID: process.PPID, ProcessGroupID: process.ProcessGroupID, StartIdentity: process.StartIdentity, Executable: process.Executable, CWD: process.CWD, TTY: process.TTY}
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
	if o.lockPath == "" {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lockFile != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(o.lockPath), 0o700); err != nil {
		return fmt.Errorf("create observer lock directory: %w", err)
	}
	file, err := openObserverLock(o.lockPath)
	if err != nil {
		return fmt.Errorf("observer already running or lock unavailable: %w", err)
	}
	o.lockFile = file
	return nil
}

//nolint:funcorder // private helpers are grouped with lock and reconciliation internals
func (o *Observer) releaseLock() {
	o.mu.Lock()
	file := o.lockFile
	o.lockFile = nil
	o.mu.Unlock()
	if file == nil {
		return
	}
	_ = closeObserverLock(file)
	_ = removeObserverLock(o.lockPath)
}

//nolint:cyclop,funcorder // health persistence handles degraded categories and atomic writes
func (o *Observer) recordHealth(at time.Time, degraded bool, category string, err error, result Result) {
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
	if shouldWrite {
		o.lastHealthWrite = at
	}
	o.mu.Unlock()
	if !shouldWrite || o.healthPath == "" {
		return
	}
	data, marshalErr := json.MarshalIndent(health, "", "  ")
	if marshalErr != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(o.healthPath), 0o700); err != nil {
		return
	}
	tmp := o.healthPath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, o.healthPath)
}

func (o *Observer) Health() Health {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.health
}

func (r Result) String() string {
	return fmt.Sprintf("observations=%d sessions=%d processes=%d panes=%d", r.Observations, r.Sessions, r.Processes, r.Panes)
}
