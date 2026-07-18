package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	defaultBinary = "agent-sessions"
	managedMarker = harnesspkg.ManagedMarker
)

var (
	errUnsupportedHarness = errors.New("unsupported harness")
	errForeignFile        = errors.New("file exists and is not managed by agent-sessions")
	errInstallFailed      = errors.New("one or more integrations failed to install")
)

var allHarnesses = installableHarnesses()

// AllHarnesses returns a snapshot of the installable harness catalog.
func AllHarnesses() []registry.Harness {
	return slices.Clone(allHarnesses)
}

type Options struct {
	Harness      registry.Harness
	Binary       string
	TargetBinary string
	DryRun       bool
	Force        bool
	UseShim      bool
}

type Result struct {
	Harness  string `json:"harness"`
	Path     string `json:"path"`
	Changed  bool   `json:"changed"`
	Message  string `json:"message"`
	NextStep string `json:"next_step,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Error    string `json:"error,omitempty"`
}

func Run(options Options) (Result, error) {
	return RunContext(context.Background(), options)
}

// RunContext installs one integration while honoring caller cancellation.
func RunContext(ctx context.Context, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("install integration context: %w", err)
	}
	if options.Binary == "" {
		options.Binary = defaultBinary
	}

	return installHarnessAdapter(ctx, options)
}

func RunAll(options Options) ([]Result, error) {
	return RunAllContext(context.Background(), options)
}

// RunAllContext installs every integration while honoring caller cancellation.
func RunAllContext(ctx context.Context, options Options) ([]Result, error) {
	harnesses := AllHarnesses()
	results := make([]Result, 0, len(harnesses))
	failures := make([]string, 0)

	for _, harness := range harnesses {
		nextOptions := options
		nextOptions.Harness = harness

		result, err := RunContext(ctx, nextOptions)
		if err != nil {
			result = Result{
				Harness:  string(harness),
				Path:     "",
				Changed:  false,
				Message:  "install failed",
				NextStep: "",
				Snippet:  "",
				Error:    err.Error(),
			}
			failures = append(failures, string(harness))
		}

		results = append(results, result)
	}

	if len(failures) > 0 {
		return results, fmt.Errorf("%w: %s", errInstallFailed, strings.Join(failures, ", "))
	}

	return results, nil
}

func installableHarnesses() []registry.Harness {
	harnesses := make([]registry.Harness, 0, len(harnesspkg.All()))
	for _, adapter := range harnesspkg.All() {
		if _, ok := adapter.(harnesspkg.Installable); ok {
			harnesses = append(harnesses, adapter.Definition().ID)
		}
	}

	return harnesses
}

func fileNeedsUpdate(path string, content string, force bool) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}

		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	if string(current) == content {
		return false, nil
	}

	if !force && !strings.Contains(string(current), managedMarker) {
		return false, fmt.Errorf("%w: %s; pass --force to replace it", errForeignFile, path)
	}

	return true, nil
}
