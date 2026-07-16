package agentstate

import (
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

type Authority string

const (
	AuthorityHook   Authority = "hook"
	AuthorityScreen Authority = "screen"
)

type Policy struct {
	Primary          Authority
	ScreenFallback   bool
	IntegrationValue string
}

type HookEvaluation struct {
	Active         bool
	Fresh          bool
	ProcessMatches bool
	Reason         string
}

func PolicyFor(harness registry.Harness) Policy {
	switch harness {
	case registry.HarnessCodex, registry.HarnessClaude:
		return Policy{Primary: AuthorityScreen, ScreenFallback: false, IntegrationValue: ""}
	case registry.HarnessOpenCode:
		return Policy{Primary: AuthorityHook, ScreenFallback: true, IntegrationValue: "opencode-plugin"}
	case registry.HarnessPi:
		return Policy{Primary: AuthorityHook, ScreenFallback: true, IntegrationValue: "pi-extension"}
	case registry.HarnessCursor, registry.HarnessCopilot, registry.HarnessCline, registry.HarnessKimiCode,
		registry.HarnessGrok, registry.HarnessGoose, registry.HarnessOmp, registry.HarnessAgy,
		registry.HarnessKilo, registry.HarnessDroid:
		return Policy{Primary: AuthorityHook, ScreenFallback: false, IntegrationValue: ""}
	default:
		return Policy{Primary: AuthorityHook, ScreenFallback: false, IntegrationValue: ""}
	}
}

func SupportsScreen(harness registry.Harness) bool {
	switch harness {
	case registry.HarnessCodex, registry.HarnessClaude, registry.HarnessOpenCode, registry.HarnessPi:
		return true
	case registry.HarnessCursor, registry.HarnessCopilot, registry.HarnessCline, registry.HarnessKimiCode,
		registry.HarnessGrok, registry.HarnessGoose, registry.HarnessOmp, registry.HarnessAgy,
		registry.HarnessKilo, registry.HarnessDroid:
		return false
	default:
		return false
	}
}

func EvaluateHook(session registry.Session, now time.Time) HookEvaluation {
	policy := PolicyFor(session.Harness)
	if policy.Primary != AuthorityHook || policy.IntegrationValue == "" {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "hook_not_activity_authority"}
	}
	native := session.Observations.Native
	if native == nil {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "integration_report_missing"}
	}
	if native.Attributes["agent_sessions_integration"] != policy.IntegrationValue {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "integration_identity_mismatch"}
	}
	if native.Activity == nil || *native.Activity == registry.ActivityUnknown {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "integration_activity_missing"}
	}
	if nativeEnded(native) || session.Presence == registry.PresenceGone {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "integration_ended"}
	}
	if session.Process == nil {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "agent_process_missing"}
	}
	if !native.Process.Equal(*session.Process) {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: false, Reason: "agent_process_replaced"}
	}
	if reason := invalidHookTimeReason(native.ObservedAt, now); reason != "" {
		return HookEvaluation{Active: false, Fresh: false, ProcessMatches: true, Reason: reason}
	}
	return HookEvaluation{Active: true, Fresh: true, ProcessMatches: true, Reason: "matching_live_process_report"}
}

func invalidHookTimeReason(observedAt time.Time, now time.Time) string {
	if observedAt.After(now) {
		return "integration_observation_from_future"
	}
	if now.Sub(observedAt) > registry.IntegrationActivityLease {
		return "integration_report_stale"
	}
	return ""
}

func HookIsActive(session registry.Session, now time.Time) bool {
	return EvaluateHook(session, now).Active
}

func ShouldDetectScreen(session registry.Session, now time.Time) bool {
	policy := PolicyFor(session.Harness)
	if policy.Primary == AuthorityScreen {
		return true
	}
	return policy.ScreenFallback && !HookIsActive(session, now)
}

func nativeEnded(native *registry.NativeObservation) bool {
	if native.Presence != nil && *native.Presence == registry.PresenceGone {
		return true
	}
	return native.Lifecycle != nil && *native.Lifecycle == registry.NativeLifecycleEnd
}
