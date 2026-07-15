package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	ManagedVersion       = 3
	ManagedMarker        = "agent-sessions managed observer service"
	managedVersion       = ManagedVersion
	managedMarker        = ManagedMarker
	defaultInterval      = 3 * time.Second
	serviceDirectoryMode = 0o755
)

var (
	ErrForeign             = errors.New("service path contains foreign content")
	ErrUnsupported         = errors.New("observer service is unsupported on this platform")
	errUnsupportedExecutor = errors.New("unsupported command executor")
	errBinaryRequired      = errors.New("binary is required")
	errStorePathRequired   = errors.New("store path is required")
	errIntervalPositive    = errors.New("interval must be positive")
	errGraceNonnegative    = errors.New("grace period must be nonnegative")
)

// Options controls the managed observer service.
type Options struct {
	Binary      string
	StorePath   string
	Interval    time.Duration
	GracePeriod time.Duration
	DryRun      bool
}

// Result describes the service state after an operation.
type Result struct {
	Platform       string `json:"platform"`
	Manager        string `json:"manager"`
	ManagedPath    string `json:"managed_path"`
	ManagedVersion int    `json:"managed_version"`
	Path           string `json:"-"`
	Version        int    `json:"-"`
	Installed      bool   `json:"installed"`
	Current        bool   `json:"current"`
	Running        bool   `json:"running"`
	Changed        bool   `json:"changed"`
	Message        string `json:"message"`
}

// CommandExecutor runs a manager command without invoking a shell.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// CommandFunc adapts a function into a CommandExecutor.
type CommandFunc func(context.Context, string, ...string) ([]byte, error)

func (f CommandFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

// Runner is the injectable manager command boundary.
type Runner = CommandExecutor

// Service manages the platform-native observer service.
type Service struct {
	executor CommandExecutor
}

type osCommandExecutor struct{}

func (osCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("execute %s: %w", name, err)
	}
	return output, nil
}

type errorExecutor struct{ err error }

func (e errorExecutor) Run(context.Context, string, ...string) ([]byte, error) { return nil, e.err }

// New constructs a service using an injected command executor. It also accepts
// the common error-only Run forms to keep command fakes small.
func New(executor any) *Service {
	if executor == nil {
		return &Service{executor: osCommandExecutor{}}
	}
	if runner, ok := executor.(CommandExecutor); ok {
		return &Service{executor: runner}
	}
	if runner, ok := executor.(func(context.Context, string, ...string) ([]byte, error)); ok {
		return &Service{executor: CommandFunc(runner)}
	}
	if runner, ok := executor.(interface {
		Run(ctx context.Context, name string, args ...string) error
	}); ok {
		return &Service{executor: errorOnlyContextRunner{runner: runner}}
	}
	if runner, ok := executor.(interface {
		Run(name string, args ...string) error
	}); ok {
		return &Service{executor: errorOnlyRunner{runner: runner}}
	}
	return &Service{executor: errorExecutor{err: fmt.Errorf("%T: %w", executor, errUnsupportedExecutor)}}
}

type errorOnlyContextRunner struct {
	runner interface {
		Run(ctx context.Context, name string, args ...string) error
	}
}

func (e errorOnlyContextRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	err := e.runner.Run(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", name, err)
	}
	return nil, nil
}

type errorOnlyRunner struct {
	runner interface {
		Run(name string, args ...string) error
	}
}

func (e errorOnlyRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	err := e.runner.Run(name, args...)
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", name, err)
	}
	return nil, nil
}

var defaultService = New(nil)

func Install(ctx context.Context, options Options) (Result, error) {
	return defaultService.Install(ctx, options)
}

func Update(ctx context.Context, options Options) (Result, error) {
	return defaultService.Update(ctx, options)
}

func Uninstall(ctx context.Context, options Options) (Result, error) {
	return defaultService.Uninstall(ctx, options)
}

func Status(ctx context.Context, options Options) (Result, error) {
	return defaultService.Status(ctx, options)
}

func (s *Service) Install(ctx context.Context, options Options) (Result, error) {
	return s.apply(ctx, options, false)
}

func (s *Service) Update(ctx context.Context, options Options) (Result, error) {
	return s.apply(ctx, options, true)
}

func (s *Service) Uninstall(ctx context.Context, options Options) (Result, error) {
	backend, err := platformBackend(options)
	if err != nil {
		return Result{}, err
	}
	result := backend.describe()
	content, readErr := os.ReadFile(result.Path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			result.Message = "not installed"
			return result, nil
		}
		return result, fmt.Errorf("read managed service: %w", readErr)
	}
	if !isManaged(string(content)) {
		return result, fmt.Errorf("%w: %s", ErrForeign, result.Path)
	}
	result.Installed, result.Current = true, string(content) == backend.content()
	if options.DryRun {
		result.Message = "would uninstall"
		return result, nil
	}
	if err := backend.unload(ctx, s.executor); err != nil {
		return result, err
	}
	if err := os.Remove(result.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("remove managed service: %w", err)
	}
	if err := backend.reload(ctx, s.executor); err != nil {
		return result, err
	}
	result.Installed, result.Current, result.Changed, result.Message = false, false, true, "uninstalled"
	return result, nil
}

func (s *Service) Status(ctx context.Context, options Options) (Result, error) {
	backend, err := platformBackend(options)
	if err != nil {
		return Result{}, err
	}
	result := backend.describe()
	content, readErr := os.ReadFile(result.Path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			result.Message = "not installed"
			return result, nil
		}
		return result, fmt.Errorf("read managed service: %w", readErr)
	}
	result.Installed = true
	result.Current = string(content) == backend.content()
	if !isManaged(string(content)) {
		return result, fmt.Errorf("%w: %s", ErrForeign, result.Path)
	}
	running, message := backend.running(ctx, s.executor)
	result.Running, result.Message = running, message
	if !result.Current {
		result.Message = "stale; " + result.Message
	} else if result.Running && result.Message == "" {
		result.Message = "running"
	}
	return result, nil
}

//nolint:gocognit,cyclop // service installation coordinates platform manager and atomic file transitions
func (s *Service) apply(ctx context.Context, options Options, update bool) (Result, error) {
	backend, err := platformBackend(options)
	if err != nil {
		return Result{}, err
	}
	result := backend.describe()
	content, readErr := os.ReadFile(result.Path)
	installed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return result, fmt.Errorf("read managed service: %w", readErr)
	}
	if installed && !isManaged(string(content)) {
		return result, fmt.Errorf("%w: %s", ErrForeign, result.Path)
	}
	result.Installed = installed
	result.Current = installed && string(content) == backend.content()
	if installed && result.Current {
		running, _ := backend.running(ctx, s.executor)
		if running {
			result.Running = true
			result.Message = "already enabled"
			return result, nil
		}
		if options.DryRun {
			result.Message = "would start"
			return result, nil
		}
		if err := backend.load(ctx, s.executor); err != nil {
			return result, err
		}
		result.Running, result.Changed, result.Message = true, true, "started"
		return result, nil
	}
	if installed && !result.Current && !update {
		result.Message = "stale; run update"
		return result, nil
	}
	if options.DryRun {
		if installed {
			result.Message = "would update"
		} else {
			result.Message = "would install"
		}
		return result, nil
	}
	if err := writeAtomic(result.Path, []byte(backend.content()), 0o644); err != nil {
		return result, err
	}
	if err := backend.reload(ctx, s.executor); err != nil {
		return result, err
	}
	if installed {
		if err := backend.restart(ctx, s.executor); err != nil {
			return result, err
		}
	} else if err := backend.load(ctx, s.executor); err != nil {
		return result, err
	}
	result.Installed, result.Current, result.Running, result.Changed, result.Message = true, true, true, true, "installed"
	if installed {
		result.Message = "updated"
	}
	return result, nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), serviceDirectoryMode); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-sessions-service-*")
	if err != nil {
		return fmt.Errorf("create temporary service: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set service mode: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write service: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync service: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close service: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace service: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open service directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync service directory: %w", err)
	}
	return nil
}

func isManaged(content string) bool {
	if !strings.Contains(content, managedMarker) {
		return false
	}
	for line := range strings.Lines(content) {
		switch strings.TrimSpace(line) {
		case "# version: 2", "# version: 3", "<!-- version: 2 -->", "<!-- version: 3 -->":
			return true
		}
	}
	return false
}

func managerMissing(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "not loaded") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "could not find")
}

func wrapManagerError(action string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w (%s)", action, err, message)
}

func normalizeOptions(options Options) (Options, error) {
	if options.Binary == "" {
		return options, errBinaryRequired
	}
	if options.StorePath == "" {
		return options, errStorePathRequired
	}
	binary, err := filepath.Abs(options.Binary)
	if err != nil {
		return options, fmt.Errorf("resolve binary: %w", err)
	}
	store, err := filepath.Abs(options.StorePath)
	if err != nil {
		return options, fmt.Errorf("resolve store: %w", err)
	}
	if options.Interval == 0 {
		options.Interval = defaultInterval
	}
	if options.Interval <= 0 {
		return options, errIntervalPositive
	}
	if options.GracePeriod < 0 {
		return options, errGraceNonnegative
	}
	options.Binary, options.StorePath = binary, store
	return options, nil
}
