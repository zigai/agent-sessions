package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

// IntegrationStatus describes the managed artifacts for one harness integration.
type IntegrationStatus struct {
	Harness registry.Harness `json:"harness"`
	Status  ArtifactStatus   `json:"status"`
	Paths   []string         `json:"paths"`
	Message string           `json:"message"`
}

// Inspect reports whether a harness integration or its shim fallback is current.
func Inspect(harnessID registry.Harness, binary string) (IntegrationStatus, error) {
	return InspectContext(context.Background(), harnessID, binary)
}

// InspectContext reports whether a harness integration is current, honoring
// cancellation while consulting native harness CLIs.
func InspectContext(ctx context.Context, harnessID registry.Harness, binary string) (IntegrationStatus, error) {
	adapter, ok := harnesspkg.Find(harnessID)
	if !ok {
		return IntegrationStatus{}, fmt.Errorf("%w: %q", errUnsupportedHarness, harnessID)
	}
	installer, ok := adapter.(harnesspkg.Installable)
	if !ok {
		return IntegrationStatus{}, fmt.Errorf("%w: %q", errUnsupportedHarness, harnessID)
	}
	plan := installer.InstallPlan(binary)
	paths := planPaths(plan)
	shimPath := filepath.Join(registry.DefaultStateDir(), "shims", string(harnessID))
	paths = append(paths, shimPath)
	result := IntegrationStatus{Harness: harnessID, Status: ArtifactMissing, Paths: paths, Message: "not installed"}
	if len(paths) == 0 {
		return result, nil
	}
	result.Status = ArtifactCurrent
	result.Message = "current"
	for _, action := range plan.Actions {
		statuses, err := inspectAction(ctx, action)
		if err != nil {
			return IntegrationStatus{}, err
		}
		for _, item := range statuses {
			mergeInspectedArtifact(&result, item)
			if result.Status == ArtifactForeign {
				return result, nil
			}
		}
	}
	if result.Status == ArtifactCurrent {
		//nolint:contextcheck // the legacy installer API has no context; native inspection above honors ctx
		dryRun, err := Run(Options{Harness: harnessID, Binary: binary, TargetBinary: "", DryRun: true, Force: false, UseShim: false})
		if err != nil {
			return IntegrationStatus{}, fmt.Errorf("checking desired integration state: %w", err)
		}
		if dryRun.Changed {
			result.Status = ArtifactStale
			result.Message = "managed integration differs from the desired configuration"
		}
	}
	return mergeShimStatus(result, shimPath)
}

func mergeShimStatus(result IntegrationStatus, path string) (IntegrationStatus, error) {
	status, err := ClassifyArtifact(path)
	if err != nil {
		return IntegrationStatus{}, err
	}
	if status == ArtifactForeign {
		result.Status, result.Message = status, "foreign content at "+path
		return result, nil
	}
	if result.Status == ArtifactMissing && (status == ArtifactCurrent || status == ArtifactStale) {
		result.Status = status
		result.Message = "shim fallback is " + string(status)
	}
	return result, nil
}

func mergeInspectedArtifact(result *IntegrationStatus, item inspectedArtifact) {
	switch item.status {
	case ArtifactForeign:
		result.Status, result.Message = item.status, "foreign content at "+item.path
	case ArtifactStale:
		result.Status, result.Message = item.status, "stale artifact at "+item.path
	case ArtifactMissing:
		if result.Status == ArtifactCurrent {
			result.Status, result.Message = item.status, "missing artifact at "+item.path
		}
	case ArtifactCurrent:
	}
}

type inspectedArtifact struct {
	path   string
	status ArtifactStatus
}

func inspectAction(ctx context.Context, action harnesspkg.InstallAction) ([]inspectedArtifact, error) {
	switch value := action.(type) {
	case harnesspkg.JSONCommandHooksAction:
		return inspectSharedFile(value.Plan.Path)
	case harnesspkg.CursorJSONHooksAction:
		return inspectSharedFile(value.Plan.Path)
	case harnesspkg.ManagedTextBlockAction:
		return inspectSharedFile(value.Plan.Path)
	case harnesspkg.RenderedFileAction:
		return inspectOwnedPath(value.Plan.Path)
	case harnesspkg.RenderedFilesAction:
		result := make([]inspectedArtifact, 0, len(value.Plan.Files))
		for _, file := range value.Plan.Files {
			items, err := inspectOwnedPath(filepath.Join(value.Plan.Dir, file.Name))
			if err != nil {
				return nil, err
			}
			result = append(result, items...)
		}
		return result, nil
	case harnesspkg.PluginDirectoryAction:
		return inspectPluginAction(ctx, value.Plan)
	default:
		return nil, nil
	}
}

//nolint:cyclop,gocognit // plugin status combines owned source and native/import registration state
func inspectPluginAction(ctx context.Context, plan harnesspkg.PluginDirectoryInstallPlan) ([]inspectedArtifact, error) {
	result, err := inspectOwnedPath(plan.Dir)
	if err != nil {
		return result, err
	}
	if plan.OpenClaw != nil {
		state, inspectErr := inspectOpenClawRegistration(ctx, plan)
		if inspectErr != nil {
			return nil, inspectErr
		}
		status := ArtifactMissing
		switch state {
		case openClawRegistrationCurrent:
			status = ArtifactCurrent
		case openClawRegistrationStale:
			status = ArtifactStale
		case openClawRegistrationForeign:
			status = ArtifactForeign
		case openClawRegistrationMissing:
		}
		return append(result, inspectedArtifact{path: "OpenClaw plugin " + plan.OpenClaw.PluginID, status: status}), nil
	}
	if plan.Hermes != nil {
		state, inspectErr := inspectHermesRegistration(ctx, plan)
		if inspectErr != nil {
			return nil, inspectErr
		}
		status := ArtifactMissing
		switch state {
		case hermesRegistrationCurrent:
			status = ArtifactCurrent
		case hermesRegistrationStale:
			status = ArtifactStale
		case hermesRegistrationMissing:
		}

		return append(result, inspectedArtifact{path: "Hermes plugin " + plan.Hermes.PluginID, status: status}), nil
	}
	if plan.ImportManifest == nil {
		return result, nil
	}
	manifest, err := readImportManifest(plan.ImportManifest.Path)
	if err != nil {
		return nil, err
	}
	status := ArtifactMissing
	for _, entry := range manifest.Imports {
		if entry.Name != plan.ImportManifest.Name {
			continue
		}
		status = ArtifactCurrent
		if entry.Source != plan.ImportManifest.Source {
			status = ArtifactStale
		}
		for _, component := range plan.ImportManifest.Components {
			if !slices.Contains(entry.Components, component) {
				status = ArtifactStale
			}
		}
		break
	}
	return append(result, inspectedArtifact{path: plan.ImportManifest.Path, status: status}), nil
}

func inspectSharedFile(path string) ([]inspectedArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []inspectedArtifact{{path: path, status: ArtifactMissing}}, nil
		}
		return nil, fmt.Errorf("reading integration artifact %s: %w", path, err)
	}
	status := classifyArtifactContent(string(data))
	if status == ArtifactForeign {
		status = ArtifactMissing
	}
	return []inspectedArtifact{{path: path, status: status}}, nil
}

func inspectOwnedPath(path string) ([]inspectedArtifact, error) {
	status, err := ClassifyArtifact(path)
	if err != nil {
		return nil, err
	}
	return []inspectedArtifact{{path: path, status: status}}, nil
}

func planPaths(plan harnesspkg.InstallPlan) []string {
	paths := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		switch value := action.(type) {
		case harnesspkg.JSONCommandHooksAction:
			paths = append(paths, value.Plan.Path)
		case harnesspkg.CursorJSONHooksAction:
			paths = append(paths, value.Plan.Path)
		case harnesspkg.ManagedTextBlockAction:
			paths = append(paths, value.Plan.Path)
		case harnesspkg.RenderedFileAction:
			paths = append(paths, value.Plan.Path)
		case harnesspkg.RenderedFilesAction:
			for _, file := range value.Plan.Files {
				paths = append(paths, filepath.Join(value.Plan.Dir, file.Name))
			}
		case harnesspkg.PluginDirectoryAction:
			paths = append(paths, value.Plan.Dir)
			if value.Plan.ImportManifest != nil {
				paths = append(paths, value.Plan.ImportManifest.Path)
			}
		}
	}
	return paths
}
