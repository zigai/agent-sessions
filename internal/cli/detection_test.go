package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

func TestDetectCommandEvaluatesOfflineScreenWithoutEchoingInput(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetIn(strings.NewReader("Would you like to run the following command? SECRET-COMMAND"))
	root.SetArgs([]string{"--json", "detect", "--harness", "codex", "--file", "-", "--config-dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var decision map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	if decision["activity"] != "waiting" || decision["rule_id"] != "permission_prompt" {
		t.Fatalf("decision = %#v", decision)
	}
	if evidence, ok := decision["evidence"].([]any); !ok || len(evidence) == 0 {
		t.Fatalf("detect output omitted rule evidence: %#v", decision)
	}
	if strings.Contains(stdout.String(), "SECRET-COMMAND") {
		t.Fatalf("detect output leaked screen input: %s", stdout.String())
	}
}

func TestDetectCommandRejectsOversizedInput(t *testing.T) {
	t.Parallel()
	root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	root.SetIn(strings.NewReader(strings.Repeat("x", maxOfflineScreenBytes+1)))
	root.SetArgs([]string{"detect", "--harness", "pi", "--file", "-", "--config-dir", t.TempDir()})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), errDetectFileTooLarge.Error()) {
		t.Fatalf("detect error = %v, want size error", err)
	}
}

func TestSameTmuxServerDoesNotTreatMissingIdentityAsWildcard(t *testing.T) {
	t.Parallel()
	if !sameTmuxServer("", "default") || sameTmuxServer("-L:work", "") || sameTmuxServer("-L:work", "-L:other") {
		t.Fatal("tmux server matching was not conservative")
	}
}

func TestExplainReportsFallbackReasonForInactiveIntegration(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/state.json"
	store := registry.NewFileStore(path)
	at := time.Now().UTC()
	process := registry.ProcessIdentity{PID: 654, ProcessGroupID: 654, Foreground: true, StartIdentity: "boot:654", Executable: "pi", TTY: "/dev/pts/not-live"}
	presence := registry.PresenceLive
	idle := registry.ActivityIdle
	tmux := registry.TmuxContext{Inside: true, ServerSocket: "-L:not-live", SessionID: "$9", SessionName: "agents", WindowID: "@9", WindowIndex: "0", PaneID: "%99", PaneIndex: "0", PanePID: 654, PaneTTY: process.TTY}
	_, err := store.Observe(context.Background(), registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: registry.HarnessPi, Identity: registry.ObservationIdentity{SessionID: "pi-inactive"}, Presence: &presence, Activity: &idle, NativeEvent: "agent_settled", Process: &process, Tmux: &tmux, Attributes: map[string]string{"agent_sessions_integration": "old-extension"}, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "explain", "--pane", "%99"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["selected_authority"] != "screen" || result["fallback_reason"] != "integration_identity_mismatch" || result["final_activity"] != "unknown" {
		t.Fatalf("fallback explanation = %#v", result)
	}
}

func TestExplainReportsActiveHookAuthorityByPane(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/state.json"
	store := registry.NewFileStore(path)
	at := time.Now().UTC()
	process := registry.ProcessIdentity{PID: 321, ProcessGroupID: 321, Foreground: true, StartIdentity: "boot:321", Executable: "pi", TTY: "/dev/pts/3"}
	presence := registry.PresenceLive
	idle := registry.ActivityIdle
	tmux := registry.TmuxContext{Inside: true, ServerSocket: "default", SessionID: "$1", SessionName: "agents", WindowID: "@1", WindowIndex: "1", PaneID: "%3", PaneIndex: "1", PanePID: 10, PaneTTY: "/dev/pts/3"}
	_, err := store.Observe(context.Background(), registry.Observation{Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Harness: registry.HarnessPi, Identity: registry.ObservationIdentity{SessionID: "pi-session"}, Presence: &presence, Activity: &idle, NativeEvent: "agent_end", Process: &process, Tmux: &tmux, Attributes: map[string]string{"agent_sessions_integration": "pi-extension"}, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "explain", "--pane", "%3"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["selected_authority"] != "hook" || result["process_match"] != "foreground_tty_process" || result["final_activity"] != "idle" {
		t.Fatalf("explain result = %#v", result)
	}
	hook, ok := result["hook"].(map[string]any)
	if !ok || hook["active"] != true || hook["fresh"] != true || hook["freshness_reason"] != "matching_live_process_report" || hook["integration"] != "pi-extension" {
		t.Fatalf("hook explanation = %#v", result["hook"])
	}
}
