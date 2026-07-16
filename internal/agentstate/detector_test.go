package agentstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestGoldenScreenFixtures(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "golden_screens.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Agent  string            `json:"agent"`
		Name   string            `json:"name"`
		Screen string            `json:"screen"`
		Want   registry.Activity `json:"want"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Agent+"/"+fixture.Name, func(t *testing.T) {
			t.Parallel()
			harness, err := registry.NormalizeHarness(fixture.Agent)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := (Loader{ConfigDir: t.TempDir()}).Load(harness)
			if err != nil {
				t.Fatal(err)
			}
			decision := Evaluate(manifest, NormalizeSnapshot(fixture.Screen, fixture.Agent))
			if decision.Activity != fixture.Want {
				t.Fatalf("decision = %#v, want %s", decision, fixture.Want)
			}
		})
	}
}

func TestBundledManifestsClassifyTargetAgents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		harness registry.Harness
		screen  string
		want    registry.Activity
		rule    string
	}{
		{registry.HarnessCodex, "› implement this\n87% context left", registry.ActivityIdle, "input_prompt"},
		{registry.HarnessCodex, "Would you like to run the following command?", registry.ActivityWaiting, "permission_prompt"},
		{registry.HarnessClaude, "Thinking… esc to interrupt", registry.ActivityRunning, "working_interruptible"},
		{registry.HarnessClaude, "❯ \n? for shortcuts", registry.ActivityIdle, "input_prompt"},
		{registry.HarnessOpenCode, "Permission required: allow / deny", registry.ActivityWaiting, "permission_prompt"},
		{registry.HarnessOpenCode, "Ask anything", registry.ActivityIdle, "input_prompt"},
		{registry.HarnessPi, "Working · esc to interrupt", registry.ActivityRunning, "working_interruptible"},
		{registry.HarnessPi, "Type a message · Enter to send", registry.ActivityIdle, "input_prompt"},
	}
	for _, test := range tests {
		t.Run(string(test.harness)+"/"+test.rule, func(t *testing.T) {
			t.Parallel()
			manifest, err := (Loader{ConfigDir: t.TempDir()}).Load(test.harness)
			if err != nil {
				t.Fatal(err)
			}
			decision := Evaluate(manifest, NormalizeSnapshot(test.screen, ""))
			if decision.Activity != test.want || decision.RuleID != test.rule {
				t.Fatalf("decision = %#v, want activity %q rule %q", decision, test.want, test.rule)
			}
		})
	}
}

func TestNormalizeSnapshotStripsTerminalEscapesAndBoundsHistory(t *testing.T) {
	t.Parallel()
	lines := make([]string, maxSnapshotLines+5)
	for index := range lines {
		lines[index] = "line"
	}
	lines[len(lines)-1] = "\x1b[31mREADY\x1b[0m"
	snapshot := NormalizeSnapshot(strings.Join(lines, "\n"), "\x1b]0;Codex\a")
	if len(snapshot.Lines) != maxSnapshotLines || snapshot.Lines[len(snapshot.Lines)-1] != "READY" || snapshot.Title != "" {
		t.Fatalf("normalized snapshot = %#v", snapshot)
	}
	blankRows := NormalizeSnapshot("permission\n\n\n", "")
	if len(blankRows.Lines) != 3 || blankRows.Lines[0] != "permission" || blankRows.Lines[1] != "" || blankRows.Lines[2] != "" {
		t.Fatalf("trailing blank rows were not preserved: %#v", blankRows)
	}
}

func TestDetectorIsConservativeWhenNoRuleMatches(t *testing.T) {
	t.Parallel()
	manifest, err := (Loader{ConfigDir: t.TempDir()}).Load(registry.HarnessCodex)
	if err != nil {
		t.Fatal(err)
	}
	decision := Evaluate(manifest, NormalizeSnapshot("ordinary shell output", "shell"))
	if decision.Activity != registry.ActivityUnknown || decision.Reason != "no_rule_matched" {
		t.Fatalf("decision = %#v, want unknown/no_rule_matched", decision)
	}
}

func TestRulePriorityRegionRegexAndExclusion(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(`version=1
agent="codex"
[[rules]]
id="low"
state="idle"
priority=1
region="all"
any=["READY"]
[[rules]]
id="high"
state="waiting"
priority=20
region="bottom:2"
all=["Approval"]
none=["do not prompt"]
regex_all=["APPROVAL"]
regex_any=["ALLOW|DENY"]
regex_none=["RESOLVED"]
title_any=["codex"]
title_regex_any=["^CODEX"]
`), registry.HarnessCodex)
	if err != nil {
		t.Fatal(err)
	}
	decision := Evaluate(manifest, NormalizeSnapshot("READY\nApproval needed: Allow or Deny", "Codex task"))
	if decision.RuleID != "high" || decision.Activity != registry.ActivityWaiting {
		t.Fatalf("decision = %#v, want high/waiting", decision)
	}
	if excluded := Evaluate(manifest, NormalizeSnapshot("READY\nApproval needed: Allow or Deny; do not prompt", "Codex task")); excluded.RuleID != "low" {
		t.Fatalf("literal exclusion did not reject high rule: %#v", excluded)
	}
	manifest.Rules[0].Priority = 20
	manifest.Rules[1].Priority = 20
	if stable := Evaluate(manifest, NormalizeSnapshot("READY\nApproval needed: Allow or Deny", "Codex task")); stable.RuleID != "low" {
		t.Fatalf("equal-priority order was not stable: %#v", stable)
	}
}

func TestManifestRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := ParseManifest([]byte("version=1\nagent='pi'\nunknown='typo'\n[[rules]]\nid='idle'\nstate='idle'\nany=['ready']\n"), registry.HarnessPi)
	if err == nil || !strings.Contains(err.Error(), errManifestInvalid.Error()) {
		t.Fatalf("unknown manifest field error = %v", err)
	}
	if _, err := ParseManifest(make([]byte, maxManifestBytes+1), registry.HarnessPi); err == nil || !strings.Contains(err.Error(), errManifestTooLarge.Error()) {
		t.Fatalf("oversized manifest error = %v", err)
	}
}

func TestLoaderUsesValidOverrideAndFallsBackFromInvalidOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pi.toml")
	if err := os.WriteFile(path, []byte("version=1\nagent='pi'\n[[rules]]\nid='custom'\nstate='idle'\nany=['CUSTOM READY']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := (Loader{ConfigDir: dir}).Load(registry.HarnessPi)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Source != path || Evaluate(manifest, NormalizeSnapshot("CUSTOM READY", "")).RuleID != "custom" {
		t.Fatalf("valid override not used: %#v", manifest)
	}
	if err := os.WriteFile(path, []byte("not valid ["), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err = (Loader{ConfigDir: dir}).Load(registry.HarnessPi)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.Source, "bundled:") || manifest.Warning == "" {
		t.Fatalf("invalid override did not fall back with warning: %#v", manifest)
	}
}

func TestDecisionJSONNeverContainsScreenContents(t *testing.T) {
	t.Parallel()
	const secret = "SUPER-SECRET-PROMPT"
	manifest, err := (Loader{ConfigDir: t.TempDir()}).Load(registry.HarnessCodex)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(Evaluate(manifest, NormalizeSnapshot(secret, secret)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("decision persisted screen contents: %s", encoded)
	}
}

//nolint:cyclop // assertions cover each integration validity and freshness reason
func TestHookAuthorityRequiresMatchingProcess(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	process := registry.ProcessIdentity{PID: 12, StartIdentity: "boot:12"}
	running := registry.ActivityRunning
	session := registry.Session{Harness: registry.HarnessPi, Presence: registry.PresenceLive, Process: &process, Observations: registry.Observations{Native: &registry.NativeObservation{Activity: &running, Attributes: map[string]string{"agent_sessions_integration": "pi-extension"}, Process: process, ObservedAt: now}}}
	if !HookIsActive(session, now) || ShouldDetectScreen(session, now) {
		t.Fatal("matching Pi extension report was not authoritative")
	}
	session.Observations.Native.Process.StartIdentity = "old"
	if HookIsActive(session, now) || !ShouldDetectScreen(session, now) {
		t.Fatal("stale Pi extension report did not fall back to screen")
	}
	session.Observations.Native.Process = process
	gone := registry.PresenceGone
	session.Observations.Native.Presence = &gone
	if evaluation := EvaluateHook(session, now); evaluation.Active || evaluation.Reason != "integration_ended" {
		t.Fatalf("ended integration evaluation = %#v", evaluation)
	}
	session.Observations.Native.Presence = nil
	session.Observations.Native.ObservedAt = now.Add(time.Second)
	if evaluation := EvaluateHook(session, now); evaluation.Active || !evaluation.ProcessMatches || evaluation.Reason != "integration_observation_from_future" {
		t.Fatalf("future integration evaluation = %#v", evaluation)
	}
	session.Observations.Native.ObservedAt = now.Add(-registry.IntegrationActivityLease - time.Second)
	if evaluation := EvaluateHook(session, now); evaluation.Active || evaluation.Fresh || !evaluation.ProcessMatches || evaluation.Reason != "integration_report_stale" || !ShouldDetectScreen(session, now) {
		t.Fatalf("stale integration evaluation = %#v", evaluation)
	}
	if PolicyFor(registry.HarnessCodex).Primary != AuthorityScreen {
		t.Fatal("Codex must be screen authoritative")
	}
}
