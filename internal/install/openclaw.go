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
	openClawInspectTimeout  = 5 * time.Second
	openClawMutationTimeout = 30 * time.Second
	openClawOutputLimit     = 64 * 1024
)

var (
	errOpenClawCLIRequired = errors.New("OpenClaw CLI is required to manage the plugin")
	errOpenClawNixMode     = errors.New("OpenClaw plugin changes are disabled by OPENCLAW_NIX_MODE")
)

type openClawRegistrationState int

const (
	openClawRegistrationMissing openClawRegistrationState = iota
	openClawRegistrationCurrent
	openClawRegistrationStale
	openClawRegistrationForeign
)

type openClawInspectReport struct {
	Plugin struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Source  string `json:"source"`
		Version string `json:"version"`
	} `json:"plugin"`
	Policy struct {
		AllowConversationAccess bool `json:"allowConversationAccess"` //nolint:tagliatelle // external OpenClaw API
	} `json:"policy"`
	Install struct {
		Source      string `json:"source"`
		SourcePath  string `json:"sourcePath"`  //nolint:tagliatelle // external OpenClaw API
		InstallPath string `json:"installPath"` //nolint:tagliatelle // external OpenClaw API
		Version     string `json:"version"`
	} `json:"install"`
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (writer *cappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := writer.limit - writer.buffer.Len()
	if remaining > 0 {
		_, _ = writer.buffer.Write(data[:min(len(data), remaining)])
	}

	return written, nil
}

func (writer *cappedBuffer) Bytes() []byte  { return writer.buffer.Bytes() }
func (writer *cappedBuffer) String() string { return writer.buffer.String() }

//nolint:cyclop // classification checks independent native registration dimensions
func inspectOpenClawRegistration(ctx context.Context, plan harnesspkg.PluginDirectoryInstallPlan) (openClawRegistrationState, error) {
	registration := plan.OpenClaw
	if registration == nil {
		return openClawRegistrationMissing, nil
	}
	if _, err := exec.LookPath(registration.Command); err != nil {
		if _, statErr := os.Stat(plan.Dir); errors.Is(statErr, os.ErrNotExist) {
			return openClawRegistrationMissing, nil
		}
		return openClawRegistrationMissing, fmt.Errorf("%w: executable %q was not found", errOpenClawCLIRequired, registration.Command)
	}

	output, err := runOpenClawCommand(ctx, openClawInspectTimeout, registration.Command, "plugins", "inspect", "--all", "--json")
	if err != nil {
		return openClawRegistrationMissing, fmt.Errorf("inspecting OpenClaw plugins: %w", err)
	}
	reports, err := decodeOpenClawInspectReports(output)
	if err != nil {
		return openClawRegistrationMissing, err
	}
	for _, report := range reports {
		if report.Plugin.ID != registration.PluginID {
			continue
		}
		if !sameCleanPath(report.Install.SourcePath, plan.Dir) {
			return openClawRegistrationForeign, nil
		}
		version := report.Plugin.Version
		if version == "" {
			version = report.Install.Version
		}
		if report.Plugin.Status != "loaded" || report.Install.Source != "path" || version != registration.Version ||
			(registration.AllowConversationAccess && !report.Policy.AllowConversationAccess) {
			return openClawRegistrationStale, nil
		}

		return openClawRegistrationCurrent, nil
	}

	return openClawRegistrationMissing, nil
}

func decodeOpenClawInspectReports(data []byte) ([]openClawInspectReport, error) {
	var reports []openClawInspectReport
	if err := json.Unmarshal(data, &reports); err == nil {
		return reports, nil
	}
	var wrapper struct {
		Plugins []openClawInspectReport `json:"plugins"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing OpenClaw plugin inspection: %w", err)
	}

	return wrapper.Plugins, nil
}

func sameCleanPath(left string, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}

	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func runOpenClawCommand(parent context.Context, timeout time.Duration, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	stdout := cappedBuffer{buffer: bytes.Buffer{}, limit: openClawOutputLimit}
	stderr := cappedBuffer{buffer: bytes.Buffer{}, limit: openClawOutputLimit}
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

func ensureOpenClawMutable() error {
	if strings.TrimSpace(os.Getenv("OPENCLAW_NIX_MODE")) == "1" {
		return errOpenClawNixMode
	}

	return nil
}

//nolint:gocognit,cyclop // native install coordinates ownership, CLI mutation, and source rollback
func installOpenClawPlugin(
	ctx context.Context,
	options Options,
	harness registry.Harness,
	plan harnesspkg.PluginDirectoryInstallPlan,
	plugin pluginDirectoryInstall,
	pluginChanged bool,
) (Result, error) {
	registration := plan.OpenClaw
	state, err := inspectOpenClawRegistration(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	if state == openClawRegistrationForeign && !options.Force {
		return Result{}, fmt.Errorf("%w: OpenClaw plugin %q; pass --force to replace it", errForeignFile, registration.PluginID)
	}
	changed := pluginChanged || state != openClawRegistrationCurrent
	label := installLabel(plan.Label, harness, "plugin")
	result := Result{Harness: string(harness), Path: plan.Dir, Changed: changed, Message: installMessage(label, changed, options.DryRun), NextStep: "", Snippet: plugin.snippet(), Error: ""}
	if !changed || options.DryRun {
		return result, nil
	}
	if err := ensureOpenClawMutable(); err != nil {
		return Result{}, err
	}
	if _, err := exec.LookPath(registration.Command); err != nil {
		return Result{}, fmt.Errorf("%w: executable %q was not found", errOpenClawCLIRequired, registration.Command)
	}

	var rollback, commit func() error
	if pluginChanged {
		rollback, commit, err = plugin.installStaged()
		if err != nil {
			return Result{}, err
		}
	}
	fail := func(cause error) error {
		if state == openClawRegistrationMissing {
			_, cleanupErr := runOpenClawCommand(ctx, openClawMutationTimeout, registration.Command, "plugins", "uninstall", registration.PluginID, "--force")
			if cleanupErr != nil {
				cause = errors.Join(cause, fmt.Errorf("cleaning up OpenClaw plugin registration: %w", cleanupErr))
			}
		}
		return rollbackPluginDirectory(rollback, cause)
	}
	if pluginChanged || state != openClawRegistrationCurrent {
		if _, err := runOpenClawCommand(ctx, openClawMutationTimeout, registration.Command, "plugins", "install", plan.Dir, "--link", "--force"); err != nil {
			return Result{}, fail(fmt.Errorf("registering OpenClaw plugin: %w", err))
		}
	}
	if registration.AllowConversationAccess {
		key := "plugins.entries." + registration.PluginID + ".hooks.allowConversationAccess"
		if _, err := runOpenClawCommand(ctx, openClawMutationTimeout, registration.Command, "config", "set", key, "true", "--strict-json"); err != nil {
			return Result{}, fail(fmt.Errorf("granting OpenClaw conversation-hook access: %w", err))
		}
	}
	if commit != nil {
		if err := commit(); err != nil {
			return Result{}, err
		}
	}

	return result, nil
}

func removeOpenClawPlugin(ctx context.Context, options Options, harnessID registry.Harness, plan harnesspkg.PluginDirectoryInstallPlan, exists bool) (Result, error) {
	state, err := inspectOpenClawRegistration(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	if state == openClawRegistrationForeign {
		return Result{}, fmt.Errorf("%w: OpenClaw plugin %q", errForeignFile, plan.OpenClaw.PluginID)
	}
	registered := state != openClawRegistrationMissing
	changed := exists || registered
	if !changed || options.DryRun {
		return removeResult(harnessID, plan.Dir, changed, options.DryRun), nil
	}
	if err := ensureOpenClawMutable(); err != nil {
		return Result{}, err
	}
	if registered {
		if _, err := runOpenClawCommand(ctx, openClawMutationTimeout, plan.OpenClaw.Command, "plugins", "uninstall", plan.OpenClaw.PluginID, "--force"); err != nil {
			return Result{}, fmt.Errorf("unregistering OpenClaw plugin: %w", err)
		}
	}
	if exists {
		if err := os.RemoveAll(plan.Dir); err != nil {
			return Result{}, fmt.Errorf("removing managed OpenClaw plugin %s: %w", plan.Dir, err)
		}
	}

	return removeResult(harnessID, plan.Dir, true, false), nil
}
