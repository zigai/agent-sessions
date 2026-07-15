package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"text/tabwriter"
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

func (app *application) newDoctorCommand() *cobra.Command {
	var verbose bool
	var all bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether agent-sessions is set up and working",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := app.runDoctor(cmd.Context(), verbose || all)
			if err := app.writeDoctorResult(result); err != nil {
				return err
			}
			if !result.OK {
				return errDoctorFailed
			}
			return nil
		},
	}
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "include all integrations and capability details")
	command.Flags().BoolVar(&all, "all", false, "include all integrations and capability details")
	return command
}

func (app *application) writeDoctorResult(result doctorResult) error {
	if app.outputJSON {
		return app.writeJSON(result)
	}
	writer := tabwriter.NewWriter(app.stdout, 0, 0, tabPadding, ' ', 0)
	if _, err := fmt.Fprintln(writer, "CHECK\tSTATUS\tMESSAGE"); err != nil {
		return fmt.Errorf("write doctor header: %w", err)
	}
	for _, check := range result.Checks {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\n", check.Name, check.Status, check.Message); err != nil {
			return fmt.Errorf("write doctor check: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush doctor checks: %w", err)
	}
	return app.writeDoctorCapabilities(result.Capabilities)
}

func (app *application) writeDoctorCapabilities(capabilities []doctorCapability) error {
	if len(capabilities) == 0 {
		return nil
	}
	if err := app.writeln("HARNESS\tSESSION_START\tSESSION_END\tRUNNING_IDLE\tWAITING_PERMISSION\tPROCESS_IDENTITY\tNATIVE_CATALOG\tTTY_TMUX"); err != nil {
		return err
	}
	for _, capability := range capabilities {
		if err := app.writef("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", capability.Harness, yesNo(capability.SessionStart), yesNo(capability.SessionEnd), yesNo(capability.RunningIdle), yesNo(capability.Waiting), yesNo(capability.ProcessIdentity), yesNo(capability.NativeCatalog), yesNo(capability.TTYTmuxContext)); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocognit,gocritic,nestif,cyclop // the doctor command intentionally reports independent checks in one ordered result
func (app *application) runDoctor(ctx context.Context, includeAll bool) doctorResult {
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
	} else if !serviceResult.Current {
		add("observer.service", doctorWarning, "managed observer service is stale; run agent-sessions monitor enable")
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
		if includeAll {
			result.Capabilities = append(result.Capabilities, doctorCapability{Harness: string(definition.ID), SessionStart: definition.Capabilities.SessionStart, SessionEnd: definition.Capabilities.SessionEnd, RunningIdle: definition.Capabilities.RunningIdle, Waiting: definition.Capabilities.WaitingPermission, ProcessIdentity: definition.Capabilities.ProcessIdentity, NativeCatalog: definition.Capabilities.NativeCatalog, TTYTmuxContext: definition.Capabilities.TTYTmuxContext})
		}
		status, message, relevant := app.integrationStatus(definition.ID)
		if includeAll || relevant {
			add("integration."+string(definition.ID), status, message)
		}
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
			result.Checks = append(result.Checks, doctorCheck{Name: "observer.reconciliation", Status: doctorWarning, Message: "monitor health is missing; run agent-sessions monitor run --once or agent-sessions monitor enable"})
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

func (app *application) integrationStatus(id registry.Harness) (doctorStatus, string, bool) {
	status, err := install.Inspect(id, defaultInstallBinary())
	if err != nil {
		return doctorError, err.Error(), true
	}
	switch status.Status {
	case install.ArtifactCurrent:
		return doctorOK, "managed integration is current", true
	case install.ArtifactMissing:
		return doctorWarning, status.Message, false
	case install.ArtifactStale:
		return doctorWarning, status.Message, true
	case install.ArtifactForeign:
		return doctorError, status.Message, true
	default:
		return doctorWarning, status.Message, true
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
