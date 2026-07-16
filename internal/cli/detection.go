package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/v2/internal/agentstate"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

const maxOfflineScreenBytes = 1 << 20

var (
	errDetectFileRequired   = errors.New("--file is required")
	errDetectFileTooLarge   = errors.New("screen input exceeds 1 MiB")
	errExplainReference     = errors.New("provide one session reference or --pane")
	errDetectionUnsupported = errors.New("screen detection is not supported")
	errTmuxPaneNotLive      = errors.New("tmux pane is not live")
)

type detectOptions struct {
	harness   string
	file      string
	title     string
	configDir string
}

type hookExplanation struct {
	Event           string    `json:"event,omitempty"`
	Integration     string    `json:"integration,omitempty"`
	ObservedAt      time.Time `json:"observed_at,omitzero"`
	Age             string    `json:"age,omitempty"`
	ProcessMatches  bool      `json:"process_matches"`
	Fresh           bool      `json:"fresh"`
	FreshnessReason string    `json:"freshness_reason"`
	Active          bool      `json:"active"`
}

type screenExplanation struct {
	Evaluated bool                `json:"evaluated"`
	Decision  agentstate.Decision `json:"decision"`
	Error     string              `json:"error,omitempty"`
}

type explainResult struct {
	SessionID         string                     `json:"session_id"`
	Harness           registry.Harness           `json:"harness"`
	PaneID            string                     `json:"pane_id,omitempty"`
	Process           *registry.ProcessIdentity  `json:"process,omitempty"`
	ProcessMatch      string                     `json:"process_match"`
	SelectedAuthority agentstate.Authority       `json:"selected_authority"`
	FallbackReason    string                     `json:"fallback_reason,omitempty"`
	FinalActivity     string                     `json:"final_activity"`
	Hook              hookExplanation            `json:"hook"`
	Screen            screenExplanation          `json:"screen"`
	RegistryActivity  *registry.Activity         `json:"registry_activity"`
	RegistryDecision  *registry.ActivityDecision `json:"registry_decision,omitempty"`
}

func (app *application) newDetectCommand() *cobra.Command {
	options := detectOptions{}
	command := &cobra.Command{Use: "detect", Short: "Evaluate an agent detection manifest against saved screen text", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return app.runDetect(cmd, options)
	}}
	command.Flags().StringVar(&options.harness, "harness", "", "agent harness (codex, claude, opencode, or pi)")
	command.Flags().StringVar(&options.file, "file", "", "screen text file, or - for stdin")
	command.Flags().StringVar(&options.title, "title", "", "optional terminal title")
	command.Flags().StringVar(&options.configDir, "config-dir", "", "detection manifest override directory")
	_ = command.MarkFlagRequired("harness")
	return command
}

func (app *application) runDetect(command *cobra.Command, options detectOptions) error {
	if options.file == "" {
		return errDetectFileRequired
	}
	harness, err := registry.NormalizeHarness(options.harness)
	if err != nil {
		return fmt.Errorf("normalize detection harness: %w", err)
	}
	if !agentstate.SupportsScreen(harness) {
		return fmt.Errorf("%w: %q", errDetectionUnsupported, harness)
	}
	data, err := readScreenInput(command, options.file)
	if err != nil {
		return err
	}
	manifest, err := (agentstate.Loader{ConfigDir: options.configDir}).Load(harness)
	if err != nil {
		return fmt.Errorf("load detection manifest: %w", err)
	}
	decision := agentstate.Evaluate(manifest, agentstate.NormalizeSnapshot(string(data), options.title))
	if app.outputJSON {
		return app.writeJSON(decision)
	}
	return app.writef("activity=%s reason=%s rule=%s manifest=%s version=%d warning=%s\n", decision.Activity, decision.Reason, decision.RuleID, decision.ManifestSource, decision.ManifestVersion, decision.Warning)
}

func readScreenInput(command *cobra.Command, path string) ([]byte, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = command.InOrStdin()
	} else {
		opened, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open screen input: %w", err)
		}
		file = opened
		defer func() { _ = file.Close() }()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxOfflineScreenBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read screen input: %w", err)
	}
	if len(data) > maxOfflineScreenBytes {
		return nil, errDetectFileTooLarge
	}
	return data, nil
}

func (app *application) newExplainCommand() *cobra.Command {
	var paneID string
	var configDir string
	command := &cobra.Command{Use: "explain [session]", Short: "Explain how an agent activity state was selected", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if (len(args) == 0) == (paneID == "") {
			return errExplainReference
		}
		var session registry.Session
		var err error
		if paneID != "" {
			session, err = app.resolvePaneSession(cmd.Context(), paneID)
		} else {
			session, err = app.resolveSession(cmd.Context(), args[0])
		}
		if err != nil {
			return err
		}
		return app.writeExplanation(cmd.Context(), session, configDir)
	}}
	command.Flags().StringVar(&paneID, "pane", "", "tmux pane id")
	command.Flags().StringVar(&configDir, "config-dir", "", "detection manifest override directory")
	return command
}

func (app *application) resolvePaneSession(ctx context.Context, paneID string) (registry.Session, error) {
	sessions, err := app.registryStore().List(ctx, registry.Filter{})
	if err != nil {
		return registry.Session{}, fmt.Errorf("list sessions: %w", err)
	}
	var match registry.Session
	found := false
	for _, session := range sessions {
		if session.Tmux.PaneID != paneID {
			continue
		}
		if found {
			return registry.Session{}, fmt.Errorf("%w: pane %q exists on multiple tmux servers", errSessionReference, paneID)
		}
		match, found = session, true
	}
	if !found {
		return registry.Session{}, registry.ErrSessionNotFound
	}
	return match, nil
}

func (app *application) writeExplanation(ctx context.Context, session registry.Session, configDir string) error {
	now := time.Now().UTC()
	policy := agentstate.PolicyFor(session.Harness)
	hookEvaluation := agentstate.EvaluateHook(session, now)
	authority := policy.Primary
	fallbackReason := ""
	if policy.Primary == agentstate.AuthorityHook && policy.ScreenFallback && !hookEvaluation.Active {
		authority = agentstate.AuthorityScreen
		fallbackReason = hookEvaluation.Reason
	}
	result := explainResult{SessionID: session.ID, Harness: session.Harness, PaneID: session.Tmux.PaneID, Process: session.Process, ProcessMatch: processMatchExplanation(session), SelectedAuthority: authority, FallbackReason: fallbackReason, FinalActivity: activityString(session.Activity), RegistryActivity: session.Activity, RegistryDecision: session.ActivityDecision}
	result.Hook = hookExplanation{Event: "", Integration: "", ObservedAt: time.Time{}, Age: "", ProcessMatches: hookEvaluation.ProcessMatches, Fresh: hookEvaluation.Fresh, FreshnessReason: hookEvaluation.Reason, Active: hookEvaluation.Active}
	if native := session.Observations.Native; native != nil {
		result.Hook.Event = native.Event
		result.Hook.Integration = native.Attributes["agent_sessions_integration"]
		result.Hook.ObservedAt = native.ObservedAt
		result.Hook.Age = now.Sub(native.ObservedAt).Round(time.Millisecond).String()
	}
	if authority == agentstate.AuthorityScreen {
		decision, evaluated, screenErr := evaluateLiveSessionScreen(ctx, session, configDir)
		result.Screen = screenExplanation{Evaluated: evaluated, Decision: decision}
		if screenErr != nil {
			result.Screen.Error = screenErr.Error()
			result.FinalActivity = string(registry.ActivityUnknown)
		}
		if evaluated {
			result.FinalActivity = string(decision.Activity)
		}
	}
	if app.outputJSON {
		return app.writeJSON(result)
	}
	return app.writef("session=%s harness=%s pane=%s process_match=%s authority=%s fallback=%s final_activity=%s registry_activity=%s hook_event=%s hook_integration=%s hook_age=%s hook_fresh=%t hook_reason=%s screen_activity=%s screen_reason=%s screen_rule=%s manifest=%s manifest_version=%d screen_warning=%s screen_error=%s\n", result.SessionID, result.Harness, result.PaneID, result.ProcessMatch, result.SelectedAuthority, result.FallbackReason, result.FinalActivity, activityString(result.RegistryActivity), result.Hook.Event, result.Hook.Integration, result.Hook.Age, result.Hook.Fresh, result.Hook.FreshnessReason, result.Screen.Decision.Activity, result.Screen.Decision.Reason, result.Screen.Decision.RuleID, result.Screen.Decision.ManifestSource, result.Screen.Decision.ManifestVersion, result.Screen.Decision.Warning, result.Screen.Error)
}

func processMatchExplanation(session registry.Session) string {
	if session.Process == nil {
		return "unavailable"
	}
	if session.Process.Foreground && session.Process.TTY != "" && session.Process.TTY == session.Tmux.PaneTTY {
		return "foreground_tty_process"
	}
	if session.Observations.Tmux != nil && session.Observations.Tmux.Process.Equal(*session.Process) {
		return "pid_start_identity"
	}
	return "unverified"
}

func evaluateLiveSessionScreen(ctx context.Context, session registry.Session, configDir string) (agentstate.Decision, bool, error) {
	panes, err := tmuxctx.ListPanes(ctx)
	if err != nil {
		return agentstate.Decision{}, false, fmt.Errorf("list tmux panes: %w", err)
	}
	for _, pane := range panes {
		if pane.Tmux.PaneID != session.Tmux.PaneID || !sameTmuxServer(pane.ServerIdentity, session.Tmux.ServerSocket) {
			continue
		}
		snapshot, err := tmuxctx.CapturePane(ctx, pane)
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("capture tmux pane: %w", err)
		}
		manifest, err := (agentstate.Loader{ConfigDir: configDir}).Load(session.Harness)
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("load detection manifest: %w", err)
		}
		return agentstate.Evaluate(manifest, agentstate.NormalizeSnapshot(snapshot.Text, snapshot.Title)), true, nil
	}
	return agentstate.Decision{}, false, fmt.Errorf("%w: %s", errTmuxPaneNotLive, session.Tmux.PaneID)
}

func sameTmuxServer(left string, right string) bool {
	if left == "" {
		left = "default"
	}
	if right == "" {
		right = "default"
	}
	return left == right
}

func activityString(activity *registry.Activity) string {
	if activity == nil {
		return "none"
	}
	return string(*activity)
}
