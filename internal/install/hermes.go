package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	hermesInspectTimeout  = 5 * time.Second
	hermesMutationTimeout = 30 * time.Second
	hermesOutputLimit     = 64 * 1024
)

var (
	errHermesCLIRequired = errors.New("hermes CLI is required to manage the plugin")
	errHermesManagedMode = errors.New("hermes plugin changes are disabled for package-manager-managed installations")
)

type hermesRegistrationState int

const (
	hermesRegistrationMissing hermesRegistrationState = iota
	hermesRegistrationCurrent
	hermesRegistrationStale
)

type hermesPluginReport struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

func inspectHermesRegistration(ctx context.Context, plan harnesspkg.PluginDirectoryInstallPlan) (hermesRegistrationState, error) {
	registration := plan.Hermes
	if registration == nil {
		return hermesRegistrationMissing, nil
	}
	if _, err := exec.LookPath(registration.Command); err != nil {
		if _, statErr := os.Stat(plan.Dir); errors.Is(statErr, os.ErrNotExist) {
			return hermesRegistrationMissing, nil
		}

		return hermesRegistrationMissing, fmt.Errorf("%w: executable %q was not found", errHermesCLIRequired, registration.Command)
	}

	output, err := runHermesCommand(ctx, hermesInspectTimeout, registration.Command, "plugins", "list", "--user", "--json")
	if err != nil {
		return hermesRegistrationMissing, fmt.Errorf("inspecting Hermes plugins: %w", err)
	}
	reports := make([]hermesPluginReport, 0)
	if err := json.Unmarshal(output, &reports); err != nil {
		return hermesRegistrationMissing, fmt.Errorf("parsing Hermes plugin inspection: %w", err)
	}
	for _, report := range reports {
		if report.Name != registration.PluginID {
			continue
		}
		if report.Status == "enabled" && report.Source == "user" && report.Version == registration.Version {
			return hermesRegistrationCurrent, nil
		}

		return hermesRegistrationStale, nil
	}

	return hermesRegistrationMissing, nil
}

func runHermesCommand(parent context.Context, timeout time.Duration, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	stdout := cappedBuffer{buffer: bytes.Buffer{}, limit: hermesOutputLimit}
	stderr := cappedBuffer{buffer: bytes.Buffer{}, limit: hermesOutputLimit}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s timed out: %w", command, ctx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return nil, fmt.Errorf("%s %s: %w", command, detail, err)
		}

		return nil, fmt.Errorf("%s: %w", command, err)
	}

	return stdout.Bytes(), nil
}

func ensureHermesMutable(plan harnesspkg.PluginDirectoryInstallPlan) error {
	if strings.TrimSpace(os.Getenv("HERMES_MANAGED")) != "" {
		return errHermesManagedMode
	}
	managedMarker := filepath.Join(filepath.Dir(filepath.Dir(plan.Dir)), ".managed")
	if _, err := os.Stat(managedMarker); err == nil {
		return errHermesManagedMode
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking Hermes managed-mode marker: %w", err)
	}

	return nil
}

//nolint:cyclop // native install coordinates ownership, activation, and source rollback
func installHermesPlugin(
	options Options,
	harness registry.Harness,
	plan harnesspkg.PluginDirectoryInstallPlan,
	plugin pluginDirectoryInstall,
	pluginChanged bool,
) (Result, error) {
	registration := plan.Hermes
	ctx := context.Background()
	state, err := inspectHermesRegistration(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	changed := pluginChanged || state != hermesRegistrationCurrent
	label := installLabel(plan.Label, harness, "plugin")
	result := Result{Harness: string(harness), Path: plan.Dir, Changed: changed, Message: installMessage(label, changed, options.DryRun), Snippet: plugin.snippet(), Error: ""}
	if !changed || options.DryRun {
		return result, nil
	}
	if err := ensureHermesMutable(plan); err != nil {
		return Result{}, err
	}
	if _, err := exec.LookPath(registration.Command); err != nil {
		return Result{}, fmt.Errorf("%w: executable %q was not found", errHermesCLIRequired, registration.Command)
	}

	var rollback, commit func() error
	if pluginChanged {
		rollback, commit, err = plugin.installStaged()
		if err != nil {
			return Result{}, err
		}
	}
	fail := func(cause error) error {
		if state != hermesRegistrationCurrent {
			_, cleanupErr := runHermesCommand(ctx, hermesMutationTimeout, registration.Command, "plugins", "disable", registration.PluginID)
			if cleanupErr != nil {
				cause = errors.Join(cause, fmt.Errorf("cleaning up Hermes plugin activation: %w", cleanupErr))
			}
		}

		return rollbackPluginDirectory(rollback, cause)
	}
	if _, err := runHermesCommand(
		ctx,
		hermesMutationTimeout,
		registration.Command,
		"plugins", "enable", registration.PluginID, "--no-allow-tool-override",
	); err != nil {
		return Result{}, fail(fmt.Errorf("enabling Hermes plugin: %w", err))
	}
	if commit != nil {
		if err := commit(); err != nil {
			return Result{}, err
		}
	}

	return result, nil
}

func removeHermesPlugin(options Options, harnessID registry.Harness, plan harnesspkg.PluginDirectoryInstallPlan, exists bool) (Result, error) {
	ctx := context.Background()
	state, err := inspectHermesRegistration(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	discovered := state != hermesRegistrationMissing
	changed := exists || discovered
	if !changed || options.DryRun {
		return removeResult(harnessID, plan.Dir, changed, options.DryRun), nil
	}
	if err := ensureHermesMutable(plan); err != nil {
		return Result{}, err
	}
	if discovered {
		if _, err := runHermesCommand(ctx, hermesMutationTimeout, plan.Hermes.Command, "plugins", "disable", plan.Hermes.PluginID); err != nil {
			return Result{}, fmt.Errorf("disabling Hermes plugin: %w", err)
		}
		if _, err := runHermesCommand(ctx, hermesMutationTimeout, plan.Hermes.Command, "plugins", "remove", plan.Hermes.PluginID); err != nil {
			return Result{}, fmt.Errorf("removing Hermes plugin: %w", err)
		}
	} else if exists {
		if err := os.RemoveAll(plan.Dir); err != nil {
			return Result{}, fmt.Errorf("removing managed Hermes plugin %s: %w", plan.Dir, err)
		}
	}

	return removeResult(harnessID, plan.Dir, true, false), nil
}
