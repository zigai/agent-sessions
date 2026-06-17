package install

import (
	"errors"
	"fmt"
	"os"
	"strings"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
)

const managedMarker = "agent-sessions managed integration"

var (
	errUnsupportedHarness = errors.New("unsupported harness")
	errForeignFile        = errors.New("file exists and is not managed by agent-sessions")
	errInstallFailed      = errors.New("one or more integrations failed to install")
)

type installer func(Options) (Result, error)

var installers = map[registry.Harness]installer{
	registry.HarnessClaude:   installClaude,
	registry.HarnessCodex:    installCodex,
	registry.HarnessCursor:   installCursor,
	registry.HarnessKimiCode: installKimiCode,
	registry.HarnessGrok:     installGrok,
	registry.HarnessPi:       installPi,
	registry.HarnessOpenCode: installOpenCode,
	registry.HarnessAgy:      installAgy,
	registry.HarnessKilo:     installKilo,
}

var AllHarnesses = installableHarnesses()

type Options struct {
	Harness      registry.Harness
	Binary       string
	TargetBinary string
	DryRun       bool
	Force        bool
	UseShim      bool
}

type Result struct {
	Harness string `json:"harness"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	Message string `json:"message"`
	Snippet string `json:"snippet,omitempty"`
	Error   string `json:"error,omitempty"`
}

func Run(options Options) (Result, error) {
	if options.Binary == "" {
		options.Binary = "agent-sessions"
	}

	runInstaller, ok := installers[options.Harness]
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", errUnsupportedHarness, options.Harness)
	}

	return runInstaller(options)
}

func RunAll(options Options) ([]Result, error) {
	results := make([]Result, 0, len(AllHarnesses))
	failures := make([]string, 0)

	for _, harness := range AllHarnesses {
		nextOptions := options
		nextOptions.Harness = harness

		result, err := Run(nextOptions)
		if err != nil {
			result = Result{
				Harness: string(harness),
				Path:    "",
				Changed: false,
				Message: "install failed",
				Snippet: "",
				Error:   err.Error(),
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
	harnesses := make([]registry.Harness, 0, len(installers))
	for _, adapter := range harnesspkg.All() {
		if !adapter.Installable {
			continue
		}
		if _, ok := installers[adapter.ID]; ok {
			harnesses = append(harnesses, adapter.ID)
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	if isSafeShellWord(value) {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func isSafeShellWord(value string) bool {
	for _, r := range value {
		if !isSafeShellRune(r) {
			return false
		}
	}

	return true
}

func isSafeShellRune(r rune) bool {
	switch {
	case r == '/', r == '.', r == '_', r == '-', r == '+', r == ':', r == '=':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	default:
		return false
	}
}
