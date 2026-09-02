package codex

const HookTrustNextStep = "start Codex and run /hooks to review and trust the aht hooks"

func HookTrustStatusNextStep(stale bool) string {
	if stale {
		return "update the Codex integration, then start Codex and run /hooks to review and trust the changed hooks"
	}
	return "start Codex and run /hooks to verify or trust the aht hooks; trust status is not available through a documented read-only interface"
}

func (codexHarness) InstallNextStep(changed bool, dryRun bool) string {
	if !changed || dryRun {
		return ""
	}
	return HookTrustNextStep
}

func (codexHarness) StatusNextStep(current bool, stale bool) string {
	if !current && !stale {
		return ""
	}
	return HookTrustStatusNextStep(stale)
}
