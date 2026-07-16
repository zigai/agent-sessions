package registry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

//nolint:cyclop // assertions cover independent hook and screen authority invariants
func TestScreenStateIsAuthoritativeForCodexAndClaude(t *testing.T) {
	t.Parallel()
	for _, harness := range []Harness{HarnessCodex, HarnessClaude} {
		t.Run(string(harness), func(t *testing.T) {
			t.Parallel()
			store := NewFileStore(t.TempDir() + "/state.json")
			at := time.Now().UTC()
			process := ProcessIdentity{PID: 100, StartIdentity: "boot:100", Executable: string(harness)}
			running := ActivityRunning
			presence := PresenceLive
			authoritative := false
			session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, Harness: harness, Identity: ObservationIdentity{SessionID: "session"}, Presence: &presence, Activity: &running, ActivityAuthoritative: &authoritative, NativeEvent: "prompt_submit", Process: &process, ObservedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			if session.Activity == nil || *session.Activity != ActivityUnknown {
				t.Fatalf("incomplete hook authored %s activity: %#v", harness, session.Activity)
			}
			if session.Observations.Native == nil || session.Observations.Native.ActivityAuthoritative == nil || *session.Observations.Native.ActivityAuthoritative {
				t.Fatalf("hook authority metadata was not preserved: %#v", session.Observations.Native)
			}
			idle := ActivityIdle
			screen := &ScreenObservation{Activity: idle, Authority: "screen", Reason: "manifest_rule", RuleID: "input_prompt", ManifestSource: "bundled", ManifestVersion: 1, Process: process, ObservedAt: at.Add(time.Second)}
			session, err = store.Observe(context.Background(), Observation{Source: ObservationSourceScreen, Evidence: ObservationEvidenceScreenState, Harness: harness, Activity: &idle, Process: &process, Screen: screen, ObservedAt: at.Add(time.Second)})
			if err != nil {
				t.Fatal(err)
			}
			if session.Activity == nil || *session.Activity != ActivityIdle || session.ActivityDecision == nil || session.ActivityDecision.Authority != "screen" {
				t.Fatalf("screen did not author activity: %#v", session)
			}
		})
	}
}

func TestStaleIntegrationDoesNotSuppressScreenFallback(t *testing.T) {
	t.Parallel()
	store := NewFileStore(t.TempDir() + "/state.json")
	at := time.Now().UTC()
	process := ProcessIdentity{PID: 150, StartIdentity: "boot:150", Executable: "pi"}
	presence := PresenceLive
	idle := ActivityIdle
	_, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, Harness: HarnessPi, Identity: ObservationIdentity{SessionID: "pi-stale"}, Presence: &presence, Activity: &idle, NativeEvent: "agent_settled", Process: &process, Attributes: map[string]string{"agent_sessions_integration": "pi-extension"}, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	waiting := ActivityWaiting
	screenAt := at.Add(IntegrationActivityLease + time.Second)
	screen := &ScreenObservation{Activity: waiting, Authority: "screen", Reason: "manifest_rule", RuleID: "permission_prompt", ManifestSource: "bundled", ManifestVersion: 1, FallbackForIntegration: "pi-extension", FallbackReason: "integration_report_stale", Process: process, ObservedAt: screenAt}
	session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceScreen, Evidence: ObservationEvidenceScreenState, Harness: HarnessPi, Identity: ObservationIdentity{SessionID: "pi-stale"}, Activity: &waiting, Process: &process, Screen: screen, ObservedAt: screenAt})
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity == nil || *session.Activity != ActivityWaiting || session.ActivityDecision == nil || session.ActivityDecision.Authority != "screen" || session.ActivityDecision.FallbackReason != "integration_report_stale" {
		t.Fatalf("stale integration suppressed fallback: %#v", session)
	}
}

func TestScreenObservationDoesNotPersistTerminalContents(t *testing.T) {
	t.Parallel()
	store := NewFileStore(t.TempDir() + "/state.json")
	at := time.Now().UTC()
	process := ProcessIdentity{PID: 101, StartIdentity: "boot:101", Executable: "codex"}
	idle := ActivityIdle
	screen := &ScreenObservation{Activity: idle, Authority: "screen", Reason: "manifest_rule", RuleID: "input_prompt", ManifestSource: "bundled", ManifestVersion: 1, Process: process, ObservedAt: at}
	session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceScreen, Evidence: ObservationEvidenceScreenState, Harness: HarnessCodex, Activity: &idle, Process: &process, Screen: screen, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "terminal contents") {
		t.Fatalf("session contains raw terminal contents: %s", encoded)
	}
}

//nolint:cyclop // regression verifies replacement and stale cross-source race invariants
func TestProcessReplacementClearsScreenActivity(t *testing.T) {
	t.Parallel()
	store := NewFileStore(t.TempDir() + "/state.json")
	at := time.Now().UTC()
	oldProcess := ProcessIdentity{PID: 102, StartIdentity: "boot:old", Executable: "codex"}
	idle := ActivityIdle
	screen := &ScreenObservation{Activity: idle, Authority: "screen", Reason: "manifest_rule", RuleID: "input_prompt", Process: oldProcess, ObservedAt: at}
	_, err := store.Observe(context.Background(), Observation{Source: ObservationSourceScreen, Evidence: ObservationEvidenceScreenState, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "same"}, Activity: &idle, Process: &oldProcess, Screen: screen, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	newProcess := ProcessIdentity{PID: 103, StartIdentity: "boot:new", Executable: "codex"}
	presence := PresenceLive
	session, err := store.Observe(context.Background(), Observation{Source: ObservationSourceNative, Evidence: ObservationEvidenceNativeEvent, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "same"}, Presence: &presence, NativeEvent: "session_start", Process: &newProcess, ObservedAt: at.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity == nil || *session.Activity != ActivityUnknown || session.Observations.Screen != nil || session.ActivityDecision == nil || session.ActivityDecision.Reason != "process_replaced" {
		t.Fatalf("replacement retained old screen activity: %#v", session)
	}
	staleIdle := ActivityIdle
	staleScreen := &ScreenObservation{Activity: staleIdle, Authority: "screen", Reason: "manifest_rule", RuleID: "input_prompt", ManifestSource: "bundled", ManifestVersion: 1, FallbackForIntegration: "", Process: oldProcess, ObservedAt: at.Add(500 * time.Millisecond)}
	if _, err := store.Observe(context.Background(), Observation{Source: ObservationSourceScreen, Evidence: ObservationEvidenceScreenState, Harness: HarnessCodex, Identity: ObservationIdentity{SessionID: "same"}, Activity: &staleIdle, Process: &oldProcess, Screen: staleScreen, ObservedAt: at.Add(500 * time.Millisecond)}); err == nil || !strings.Contains(err.Error(), ErrObservationConflict.Error()) {
		t.Fatalf("stale old-process screen error = %v, want conflict", err)
	}
	session, err = store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Process == nil || !session.Process.Equal(newProcess) || session.Activity == nil || *session.Activity != ActivityUnknown {
		t.Fatalf("stale screen restored replaced process: %#v", session)
	}
}
