package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zigai/aht/internal/agentstate"
	"github.com/zigai/aht/pkg/herdr"
	"github.com/zigai/aht/pkg/mux"
	"github.com/zigai/aht/pkg/registry"
	"github.com/zigai/aht/pkg/tmux"
	"github.com/zigai/aht/pkg/zellij"
)

var errTmuxPaneNotLive = errors.New("tmux pane is not live")

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
	Evaluated         bool                `json:"evaluated"`
	UnavailableReason string              `json:"unavailable_reason,omitempty"`
	Decision          agentstate.Decision `json:"decision"`
	Error             string              `json:"error,omitempty"`
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

func (app *application) resolvePaneSession(ctx context.Context, paneID string) (registry.Session, error) {
	sessions, err := app.registryStore().List(ctx, registry.Filter{})
	if err != nil {
		return registry.Session{}, fmt.Errorf("list sessions: %w", err)
	}
	var match registry.Session
	found := false
	for _, session := range sessions {
		if session.Multiplexer.PaneID != paneID {
			continue
		}
		if found {
			return registry.Session{}, fmt.Errorf("%w: pane %q exists in multiple multiplexer sessions", errSessionReference, paneID)
		}
		match, found = session, true
	}
	if !found {
		return registry.Session{}, registry.ErrSessionNotFound
	}
	return match, nil
}

func evaluateExplanation(ctx context.Context, session registry.Session, options infoOptions) (explainResult, error) {
	now := time.Now().UTC()
	policy := agentstate.PolicyFor(session.Harness)
	hookEvaluation := agentstate.EvaluateHook(session, now)
	authority := policy.Primary
	fallbackReason := ""
	if policy.Primary == agentstate.AuthorityHook && policy.ScreenFallback && !hookEvaluation.Active {
		authority = agentstate.AuthorityScreen
		fallbackReason = hookEvaluation.Reason
	}
	result := explainResult{SessionID: session.ID, Harness: session.Harness, PaneID: session.Multiplexer.PaneID, Process: session.Process, ProcessMatch: processMatchExplanation(session), SelectedAuthority: authority, FallbackReason: fallbackReason, FinalActivity: activityString(session.Activity), RegistryActivity: session.Activity, RegistryDecision: session.ActivityDecision}
	result.Hook = hookExplanation{Event: "", Integration: "", ObservedAt: time.Time{}, Age: "", ProcessMatches: hookEvaluation.ProcessMatches, Fresh: hookEvaluation.Fresh, FreshnessReason: hookEvaluation.Reason, Active: hookEvaluation.Active}
	if native := session.Observations.Native; native != nil {
		result.Hook.Event = native.Event
		result.Hook.Integration = native.Attributes["aht_integration"]
		result.Hook.ObservedAt = native.ObservedAt
		result.Hook.Age = now.Sub(native.ObservedAt).Round(time.Millisecond).String()
	}
	if options.disableScreenInspection {
		result.Screen.UnavailableReason = "screen_inspection_disabled"
		return result, nil
	}
	screen, finalActivity, screenErr := explanationScreen(ctx, session, options.configDir, authority, result.FinalActivity)
	result.Screen = screen
	result.FinalActivity = finalActivity
	if screenErr != nil {
		return result, fmt.Errorf("evaluate live screen: %w", screenErr)
	}
	return result, nil
}

func explanationScreen(ctx context.Context, session registry.Session, configDir string, authority agentstate.Authority, currentActivity string) (screenExplanation, string, error) {
	if authority != agentstate.AuthorityScreen {
		return screenExplanation{}, currentActivity, nil
	}
	if strings.TrimSpace(session.Multiplexer.PaneID) == "" {
		return screenExplanation{UnavailableReason: "no_live_pane"}, string(registry.ActivityUnknown), nil
	}
	decision, evaluated, err := evaluateLiveSessionScreen(ctx, session, configDir)
	screen := screenExplanation{Evaluated: evaluated, Decision: decision}
	if err != nil {
		screen.Error = err.Error()
		return screen, string(registry.ActivityUnknown), err
	}
	if evaluated {
		return screen, string(decision.Activity), nil
	}
	return screen, currentActivity, nil
}

func (app *application) writeExplanationDetails(result explainResult) error {
	return app.writeHumanDetails([]humanDetail{
		{label: "Registry activity", value: activityString(result.RegistryActivity)},
		{label: "Effective activity", value: result.FinalActivity},
		{label: "Authority", value: string(result.SelectedAuthority)},
		{label: "Process match", value: result.ProcessMatch},
		{label: "Fallback", value: result.FallbackReason},
		{label: "Hook event", value: result.Hook.Event},
		{label: "Hook integration", value: result.Hook.Integration},
		{label: "Hook age", value: result.Hook.Age},
		{label: "Hook fresh", value: strconv.FormatBool(result.Hook.Fresh)},
		{label: "Hook reason", value: result.Hook.FreshnessReason},
		{label: "Screen evaluated", value: strconv.FormatBool(result.Screen.Evaluated)},
		{label: "Screen unavailable", value: result.Screen.UnavailableReason},
		{label: "Screen activity", value: string(result.Screen.Decision.Activity)},
		{label: "Screen reason", value: result.Screen.Decision.Reason},
		{label: "Screen rule", value: result.Screen.Decision.RuleID},
		{label: "Manifest", value: result.Screen.Decision.ManifestSource},
		{label: "Manifest version", value: strconv.Itoa(result.Screen.Decision.ManifestVersion)},
		{label: "Screen warning", value: result.Screen.Decision.Warning},
		{label: "Screen error", value: result.Screen.Error},
	})
}

func processMatchExplanation(session registry.Session) string {
	if session.Process == nil {
		return "unavailable"
	}
	if session.Process.Foreground && session.Process.TTY != "" && session.Process.TTY == session.Multiplexer.PaneTTY {
		return "foreground_tty_process"
	}
	if session.Observations.Multiplexer != nil && session.Observations.Multiplexer.Process.Equal(*session.Process) {
		return "pid_start_identity"
	}
	if session.Observations.Tmux != nil && session.Observations.Tmux.Process.Equal(*session.Process) {
		return "pid_start_identity"
	}
	return "unverified"
}

//nolint:cyclop // live capture dispatches across supported native multiplexer APIs
func evaluateLiveSessionScreen(ctx context.Context, session registry.Session, configDir string) (agentstate.Decision, bool, error) {
	if session.Multiplexer.Kind != registry.MultiplexerTmux {
		pane := mux.Pane{Location: session.Multiplexer}
		var snapshot mux.ScreenSnapshot
		var err error
		switch session.Multiplexer.Kind {
		case registry.MultiplexerTmux:
			return agentstate.Decision{}, false, fmt.Errorf("%w: %s", errTmuxPaneNotLive, session.Multiplexer.PaneID)
		case registry.MultiplexerZellij:
			snapshot, err = zellij.CapturePane(ctx, pane)
		case registry.MultiplexerHerdr:
			snapshot, err = herdr.CapturePane(ctx, pane)
		default:
			return agentstate.Decision{}, false, fmt.Errorf("%w: %s", errTmuxPaneNotLive, session.Multiplexer.PaneID)
		}
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("capture %s pane: %w", session.Multiplexer.Kind, err)
		}
		manifest, err := (agentstate.Loader{ConfigDir: configDir}).Load(session.Harness)
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("load detection manifest: %w", err)
		}
		return manifest.Evaluate(agentstate.NormalizeSnapshot(snapshot.Text, snapshot.Title)), true, nil
	}
	panes, err := tmux.ListPanes(ctx)
	if err != nil {
		return agentstate.Decision{}, false, fmt.Errorf("list tmux panes: %w", err)
	}
	for _, pane := range panes {
		if pane.Tmux.PaneID != session.Tmux.PaneID || !sameTmuxServer(pane.ServerIdentity, session.Tmux.ServerSocket) {
			continue
		}
		snapshot, err := tmux.CapturePane(ctx, pane)
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("capture tmux pane: %w", err)
		}
		manifest, err := (agentstate.Loader{ConfigDir: configDir}).Load(session.Harness)
		if err != nil {
			return agentstate.Decision{}, false, fmt.Errorf("load detection manifest: %w", err)
		}
		return manifest.Evaluate(agentstate.NormalizeSnapshot(snapshot.Text, snapshot.Title)), true, nil
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
