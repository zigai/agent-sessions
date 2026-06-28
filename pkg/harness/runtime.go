package harness

import (
	"slices"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const minimumProcessArgsWithCommand = 2

type RuntimeStateAdapter interface {
	// AdjustRuntimeState normalizes a reported state using harness-specific runtime context.
	AdjustRuntimeState(
		state registry.State,
		event string,
		attributes map[string]string,
		parentArgs []string,
	) registry.State
}

type headlessRuntimePolicy struct {
	harness           registry.Harness
	eventAttributeKey string
	idleEvents        []string
	argsMatch         func([]string) bool
}

var (
	codexHeadlessRuntimePolicy = headlessRuntimePolicy{
		harness:           registry.HarnessCodex,
		eventAttributeKey: "codex_hook_event",
		idleEvents:        []string{"Stop"},
		argsMatch: func(args []string) bool {
			return argsContainCommand(args, "exec", "e", "review")
		},
	}
	grokHeadlessRuntimePolicy = headlessRuntimePolicy{
		harness:           registry.HarnessGrok,
		eventAttributeKey: "grok_hook_event",
		idleEvents:        []string{"Stop"},
		argsMatch: func(args []string) bool {
			return argsContainHeadlessPromptFlag(args, "--single", "--prompt-json", "--prompt-file")
		},
	}
	openCodeHeadlessRuntimePolicy = headlessRuntimePolicy{
		harness:           registry.HarnessOpenCode,
		eventAttributeKey: "opencode_event",
		idleEvents:        []string{"session.idle", "session.updated", "session.status", "session.error"},
		argsMatch: func(args []string) bool {
			return argsContainCommand(args, "run") && !argsContainFlag(args, "-i", "--interactive")
		},
	}
	kiloHeadlessRuntimePolicy = headlessRuntimePolicy{
		harness:           registry.HarnessKilo,
		eventAttributeKey: "kilo_event",
		idleEvents:        []string{"session.idle", "session.updated", "session.status", "session.error"},
		argsMatch: func(args []string) bool {
			return argsContainCommand(args, "run") && !argsContainFlag(args, "-i", "--interactive")
		},
	}
	kimiCodeHeadlessRuntimePolicy = headlessRuntimePolicy{
		harness:           registry.HarnessKimiCode,
		eventAttributeKey: "kimi_code_hook_event",
		idleEvents:        []string{"Stop", "StopFailure", "Interrupt"},
		argsMatch: func(args []string) bool {
			return argsContainHeadlessPromptFlag(args, "--print")
		},
	}
)

// AdjustRuntimeState lets harness adapters reinterpret hook state when native
// lifecycle events have different meanings for interactive and headless runs.
func AdjustRuntimeState(
	harness registry.Harness,
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	adapter, ok := Find(harness)
	if !ok {
		return state
	}
	runtimeAdapter, ok := adapter.(RuntimeStateAdapter)
	if !ok {
		return state
	}

	return runtimeAdapter.AdjustRuntimeState(state, event, attributes, parentArgs)
}

// HeadlessArgsForHarness reports whether parent process args identify a known
// non-interactive harness run.
func HeadlessArgsForHarness(harness registry.Harness, args []string) bool {
	policy, ok := headlessRuntimePolicyForHarness(harness)
	return ok && policy.argsMatch(args)
}

func (codexHarness) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	return codexHeadlessRuntimePolicy.AdjustRuntimeState(state, event, attributes, parentArgs)
}

func (grokHarness) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	return grokHeadlessRuntimePolicy.AdjustRuntimeState(state, event, attributes, parentArgs)
}

func (openCodeHarness) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	return openCodeHeadlessRuntimePolicy.AdjustRuntimeState(state, event, attributes, parentArgs)
}

func (kiloHarness) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	return kiloHeadlessRuntimePolicy.AdjustRuntimeState(state, event, attributes, parentArgs)
}

func (kimiCodeHarness) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	return kimiCodeHeadlessRuntimePolicy.AdjustRuntimeState(state, event, attributes, parentArgs)
}

func (policy headlessRuntimePolicy) AdjustRuntimeState(
	state registry.State,
	event string,
	attributes map[string]string,
	parentArgs []string,
) registry.State {
	if state != registry.StateIdle || !policy.argsMatch(parentArgs) {
		return state
	}
	if !eventOrAttributeMatches(event, attributes, policy.eventAttributeKey, policy.idleEvents...) {
		return state
	}

	if attributes != nil {
		prefix := headlessAttributePrefix(policy.harness)
		attributes[prefix+"_headless"] = "true"
		attributes[prefix+"_stop_state_override"] = "headless-parent"
	}

	return registry.StateExited
}

func headlessRuntimePolicyForHarness(harness registry.Harness) (headlessRuntimePolicy, bool) {
	switch harness {
	case registry.HarnessCodex:
		return codexHeadlessRuntimePolicy, true
	case registry.HarnessGrok:
		return grokHeadlessRuntimePolicy, true
	case registry.HarnessOpenCode:
		return openCodeHeadlessRuntimePolicy, true
	case registry.HarnessKilo:
		return kiloHeadlessRuntimePolicy, true
	case registry.HarnessKimiCode:
		return kimiCodeHeadlessRuntimePolicy, true
	case registry.HarnessClaude, registry.HarnessCursor, registry.HarnessPi, registry.HarnessAgy:
		return headlessRuntimePolicy{harness: "", eventAttributeKey: "", idleEvents: nil, argsMatch: nil}, false
	}

	return headlessRuntimePolicy{harness: "", eventAttributeKey: "", idleEvents: nil, argsMatch: nil}, false
}

func eventOrAttributeMatches(event string, attributes map[string]string, attributeKey string, values ...string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(event), value) {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(attributes[attributeKey]), value) {
			return true
		}
	}

	return false
}

func argsContainHeadlessPromptFlag(args []string, longFlags ...string) bool {
	for _, arg := range args {
		if arg == "-p" || strings.HasPrefix(arg, "-p") && arg != "-p" {
			return true
		}
		for _, flag := range longFlags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return true
			}
		}
	}

	return false
}

func argsContainCommand(args []string, commands ...string) bool {
	if len(args) < minimumProcessArgsWithCommand {
		return false
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if slices.Contains(commands, arg) {
			return true
		}
	}

	return false
}

func argsContainFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return true
			}
		}
	}

	return false
}

func headlessAttributePrefix(harness registry.Harness) string {
	return strings.ReplaceAll(string(harness), "-", "_")
}
