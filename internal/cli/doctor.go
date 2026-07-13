package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/install"
	"github.com/zigai/agent-sessions/internal/processinfo"
	"github.com/zigai/agent-sessions/internal/reportqueue"
	"github.com/zigai/agent-sessions/internal/service"
	"github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
)

type doctorStatus string

const (
	doctorOK      doctorStatus = "ok"
	doctorWarning doctorStatus = "warning"
	doctorError   doctorStatus = "error"
)

const (
	doctorCheckCapacity    = 9
	serviceDefaultInterval = 3 * time.Second
)

type doctorCheck struct {
	Name    string       `json:"name"`
	Status  doctorStatus `json:"status"`
	Message string       `json:"message"`
}

type doctorCapability struct {
	Harness         string `json:"harness"`
	SessionStart    bool   `json:"session_start"`
	SessionEnd      bool   `json:"session_end"`
	RunningIdle     bool   `json:"running_idle"`
	Waiting         bool   `json:"waiting_permission"`
	ProcessIdentity bool   `json:"process_identity"`
	NativeCatalog   bool   `json:"native_catalog"`
	TTYTmuxContext  bool   `json:"tty_tmux_context"`
}

type doctorResult struct {
	OK           bool               `json:"ok"`
	Checks       []doctorCheck      `json:"checks"`
	Capabilities []doctorCapability `json:"capabilities"`
}

//nolint:nestif // command rendering keeps JSON and human output branches together
func (app *application) newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check observer, registry, queue, and integrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := app.runDoctor(cmd.Context())
			if app.outputJSON {
				if err := app.writeJSON(result); err != nil {
					return err
				}
			} else {
				for _, check := range result.Checks {
					if err := app.writef("%s\t%s\t%s\n", check.Name, check.Status, check.Message); err != nil {
						return err
					}
				}
				if err := app.writeln("HARNESS\tSESSION_START\tSESSION_END\tRUNNING_IDLE\tWAITING_PERMISSION\tPROCESS_IDENTITY\tNATIVE_CATALOG\tTTY_TMUX"); err != nil {
					return err
				}
				for _, capability := range result.Capabilities {
					if err := app.writef("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", capability.Harness, yesNo(capability.SessionStart), yesNo(capability.SessionEnd), yesNo(capability.RunningIdle), yesNo(capability.Waiting), yesNo(capability.ProcessIdentity), yesNo(capability.NativeCatalog), yesNo(capability.TTYTmuxContext)); err != nil {
						return err
					}
				}
			}
			if !result.OK {
				return errDoctorFailed
			}
			return nil
		},
	}
}

//nolint:gocognit,gocritic,nestif,cyclop // the doctor command intentionally reports independent checks in one ordered result
func (app *application) runDoctor(ctx context.Context) doctorResult {
	result := doctorResult{Checks: make([]doctorCheck, 0, doctorCheckCapacity+len(harness.All())), Capabilities: make([]doctorCapability, 0, len(harness.All()))}
	add := func(name string, status doctorStatus, message string) {
		result.Checks = append(result.Checks, doctorCheck{Name: name, Status: status, Message: message})
	}

	store := app.store()
	if _, err := store.List(ctx, registry.Filter{}); err != nil {
		var unsupported *registry.UnsupportedSchemaError
		if errors.As(err, &unsupported) {
			add("store.schema", doctorError, unsupported.Error())
		} else {
			add("store.schema", doctorError, err.Error())
		}
	} else {
		add("store.schema", doctorOK, "schema_version=2")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		add("observer.platform", doctorError, "unsupported platform: "+runtime.GOOS)
	} else {
		add("observer.platform", doctorOK, runtime.GOOS)
	}
	if _, err := processinfo.List(ctx); err != nil {
		var unsupported *processinfo.UnsupportedError
		if errors.As(err, &unsupported) {
			add("observer.process-enumeration", doctorError, unsupported.Error())
		} else {
			add("observer.process-enumeration", doctorError, err.Error())
		}
	} else {
		add("observer.process-enumeration", doctorOK, "complete current-user process inventory available")
	}
	serviceResult, serviceErr := service.Status(ctx, service.Options{Binary: defaultInstallBinary(), StorePath: store.Path(), Interval: serviceDefaultInterval})
	if serviceErr != nil {
		if errors.Is(serviceErr, service.ErrUnsupported) {
			add("observer.service", doctorWarning, serviceErr.Error())
		} else {
			add("observer.service", doctorError, serviceErr.Error())
		}
	} else if !serviceResult.Installed {
		add("observer.service", doctorWarning, "managed observer service is not installed")
	} else if !serviceResult.Running {
		add("observer.service", doctorWarning, "managed observer service is stopped")
	} else {
		add("observer.service", doctorOK, "managed observer service is running")
	}
	app.addObserverReconciliationCheck(&result, store.Path())

	queue := reportqueue.New(store.Path())
	queueStatus, queueErr := queue.Status(ctx)
	if queueErr != nil {
		add("queue.backlog", doctorError, queueErr.Error())
		add("queue.retries", doctorError, queueErr.Error())
		add("queue.leases", doctorError, queueErr.Error())
		add("queue.dead-letters", doctorError, queueErr.Error())
	} else {
		if queueStatus.Ready+queueStatus.Deferred+queueStatus.Processing == 0 {
			add("queue.backlog", doctorOK, "queue is empty")
		} else {
			add("queue.backlog", doctorWarning, fmt.Sprintf("%d queued observations (%d ready, %d deferred, %d processing)", queueStatus.Pending+queueStatus.Processing, queueStatus.Ready, queueStatus.Deferred, queueStatus.Processing))
		}
		if queueStatus.Retries == 0 {
			add("queue.retries", doctorOK, "no retries pending")
		} else {
			add("queue.retries", doctorWarning, fmt.Sprintf("%d queued observations have retries", queueStatus.Retries))
		}
		if queueStatus.StaleLeases == 0 {
			add("queue.leases", doctorOK, "no stale leases")
		} else {
			add("queue.leases", doctorError, fmt.Sprintf("%d stale queue leases require recovery", queueStatus.StaleLeases))
		}
		if queueStatus.Dead == 0 {
			add("queue.dead-letters", doctorOK, "no dead letters")
		} else {
			add("queue.dead-letters", doctorError, fmt.Sprintf("%d dead letters (%d invalid envelopes)", queueStatus.Dead, queueStatus.Invalid))
		}
	}
	for _, adapter := range harness.All() {
		definition := adapter.Definition()
		result.Capabilities = append(result.Capabilities, doctorCapability{Harness: string(definition.ID), SessionStart: definition.Capabilities.SessionStart, SessionEnd: definition.Capabilities.SessionEnd, RunningIdle: definition.Capabilities.RunningIdle, Waiting: definition.Capabilities.WaitingPermission, ProcessIdentity: definition.Capabilities.ProcessIdentity, NativeCatalog: definition.Capabilities.NativeCatalog, TTYTmuxContext: definition.Capabilities.TTYTmuxContext})
		status, message := app.integrationStatus(definition.ID)
		add("integration."+string(definition.ID), status, message)
	}
	result.OK = true
	for _, check := range result.Checks {
		if check.Status == doctorError {
			result.OK = false
			break
		}
	}
	return result
}

type observerHealth struct {
	LastSuccessAt        time.Time `json:"last_success_at"`
	LastEnumerationError string    `json:"last_enumeration_error"`
}

func (app *application) addObserverReconciliationCheck(result *doctorResult, storePath string) {
	path := storePath + ".observer-health.json"
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorWarning, Message: "observer health sidecar is missing; run observe or install the service"})
			return
		}
		result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorError, Message: err.Error()})
		return
	}
	var health observerHealth
	if err := json.Unmarshal(data, &health); err != nil {
		result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorError, Message: "invalid observer health sidecar: " + err.Error()})
		return
	}
	if health.LastEnumerationError != "" {
		result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorError, Message: health.LastEnumerationError})
		return
	}
	if health.LastSuccessAt.IsZero() {
		result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorWarning, Message: "observer has not completed a successful reconciliation"})
		return
	}
	result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorOK, Message: "last successful reconciliation at " + health.LastSuccessAt.Format(time.RFC3339)})
}

func (app *application) integrationStatus(id registry.Harness) (doctorStatus, string) {
	adapter, ok := harness.Find(id)
	if !ok {
		return doctorError, "adapter is not registered"
	}
	installable, ok := adapter.(harness.Installable)
	if !ok {
		return doctorOK, "no managed integration declared"
	}
	plan := installable.InstallPlan(defaultInstallBinary())
	paths := installPlanPaths(plan)
	if len(paths) == 0 {
		return doctorWarning, "managed integration has no inspectable artifact path"
	}
	for _, path := range paths {
		status, err := install.ClassifyArtifact(path)
		if err != nil {
			return doctorError, err.Error()
		}
		switch status {
		case install.ArtifactCurrent:
		case install.ArtifactMissing:
			return doctorWarning, "managed integration is missing: " + path
		case install.ArtifactStale:
			return doctorWarning, "managed integration is stale: " + path
		case install.ArtifactForeign:
			return doctorError, "foreign integration content: " + path
		}
	}
	return doctorOK, "managed integration is current"
}

func installPlanPaths(plan harness.InstallPlan) []string {
	paths := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		switch value := action.(type) {
		case harness.JSONCommandHooksAction:
			paths = append(paths, value.Plan.Path)
		case harness.CursorJSONHooksAction:
			paths = append(paths, value.Plan.Path)
		case harness.ManagedTextBlockAction:
			paths = append(paths, value.Plan.Path)
		case harness.RenderedFileAction:
			paths = append(paths, value.Plan.Path)
		case harness.RenderedFilesAction:
			for _, file := range value.Plan.Files {
				paths = append(paths, filepath.Join(value.Plan.Dir, file.Name))
			}
		case harness.PluginDirectoryAction:
			for _, file := range value.Plan.Files {
				paths = append(paths, filepath.Join(value.Plan.Dir, file.Name))
			}
		}
	}
	return paths
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
