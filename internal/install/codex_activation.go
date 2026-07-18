package install

const codexHookTrustNextStep = "start Codex and run /hooks to review and trust the agent-sessions hooks"

func codexHookTrustStatusNextStep(status ArtifactStatus) string {
	if status == ArtifactStale {
		return "update the Codex integration, then start Codex and run /hooks to review and trust the changed hooks"
	}

	return "start Codex and run /hooks to verify or trust the agent-sessions hooks; trust status is not available through a documented read-only interface"
}
