package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zigai/agent-sessions/internal/service"
	"github.com/zigai/agent-sessions/pkg/registry"
)

func TestRootHelpShowsSimplifiedSurfaceAndHidesCompatibilityCommands(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--help"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	help := stdout.String()
	assertRootHelpGroups(t, help)
	for _, command := range []string{"setup", "list", "watch", "show", "stop", "doctor", "integrations", "monitor", "registry", "hook"} {
		if !strings.Contains(help, "  "+command) {
			t.Errorf("root help does not show %q:\n%s", command, help)
		}
	}
	for _, command := range []string{"install-hooks", "observe", "service", "report", "get", "manage", "gc", "queue", "drain", "path"} {
		if strings.Contains(help, "  "+command+" ") {
			t.Errorf("root help exposes compatibility command %q:\n%s", command, help)
		}
	}
}

func assertRootHelpGroups(t *testing.T, help string) {
	t.Helper()
	groups := map[string][]string{
		"Sessions:": {"list", "watch", "show", "stop"},
		"Setup:":    {"setup", "integrations", "hook"},
		"System:":   {"monitor", "registry", "doctor"},
	}
	for title, commands := range groups {
		start := strings.Index(help, title+"\n")
		if start < 0 {
			t.Errorf("root help does not show group %q:\n%s", title, help)
			continue
		}
		section := help[start:]
		if end := strings.Index(section, "\n\n"); end >= 0 {
			section = section[:end]
		}
		for _, command := range commands {
			if !strings.Contains(section, "  "+command) {
				t.Errorf("group %q does not contain %q:\n%s", title, command, help)
			}
		}
	}
	for _, oldTitle := range []string{"Everyday Commands:", "Configuration Commands:"} {
		if strings.Contains(help, oldTitle) {
			t.Errorf("root help still shows old group %q:\n%s", oldTitle, help)
		}
	}
}

func TestCompatibilityCommandsRemainCallable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	for _, args := range [][]string{{"--store", path, "path"}, {"--store", path, "get", "missing"}, {"--store", path, "gc", "--all"}} {
		root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(args)
		err := root.ExecuteContext(context.Background())
		if args[2] == "get" {
			if err == nil {
				t.Fatal("legacy get missing unexpectedly succeeded")
			}
			continue
		}
		if err != nil {
			t.Fatalf("legacy %s is not callable: %v", args[2], err)
		}
	}
}

func TestEveryHiddenCompatibilityCommandHasCallableHelp(t *testing.T) {
	commands := []string{"report", "get", "gc", "manage", "path", "install-hooks", "agy-hook", "drain-queue", "queue-status", "observe", "service"}
	for _, command := range commands {
		var stdout bytes.Buffer
		root := NewRootCommand(&stdout, &bytes.Buffer{})
		root.SetArgs([]string{command, "--help"})
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Errorf("%s --help failed: %v", command, err)
			continue
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("%s compatibility help missing usage: %q", command, stdout.String())
		}
	}
}

func TestRuntimeFailureDoesNotPrintUsage(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "show", "missing"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected missing session error")
	}
	if strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("runtime failure printed usage: %q", stdout.String())
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"show"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected invocation error")
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("invocation error omitted usage: %q", stdout.String())
	}
}

func TestListRejectsModeSpecificFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	tests := [][]string{
		{"--store", path, "list", "--format", "plain"},
		{"--store", path, "list", "--no-snapshot"},
		{"--store", path, "list", "--watch", "--summary"},
		{"--store", path, "list", "--watch", "--summary=false"},
		{"--store", path, "list", "--watch", "--sort", "updated"},
		{"--store", path, "list", "--summary", "--desc"},
		{"--store", path, "list", "--summary", "--absolute-time"},
		{"--store", path, "--json", "list", "--absolute-time"},
		{"--store", path, "list", "--sort", ""},
		{"--store", path, "watch", "--format", ""},
	}
	for _, args := range tests {
		root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); err == nil {
			t.Errorf("arguments unexpectedly accepted: %v", args)
		}
	}
}

func TestSubcommandFlagsAreScoped(t *testing.T) {
	tests := [][]string{
		{"integrations", "remove", "codex", "--force"},
		{"integrations", "status", "codex", "--dry-run"},
		{"monitor", "status", "--dry-run"},
		{"monitor", "disable", "--grace-period", "1s"},
		{"watch", "--summary"},
	}
	for _, args := range tests {
		root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%v error = %v, want unknown flag", args, err)
		}
	}
}

func TestIntegrationsInstallRejectsTargetBinaryWithoutShim(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"integrations", "install", "codex", "--target-binary", "/bin/codex"}, {"install-hooks", "codex", "--target-binary", "/bin/codex"}} {
		root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); !errors.Is(err, errTargetBinaryNeedsShim) {
			t.Errorf("%v target binary error = %v", args, err)
		}
	}
	for _, args := range [][]string{{"integrations", "install", "all", "--shim", "--target-binary", "/bin/agent"}, {"install-hooks", "all", "--shim", "--target-binary", "/bin/agent"}} {
		root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); !errors.Is(err, errTargetBinaryWithAll) {
			t.Errorf("%v all target binary error = %v", args, err)
		}
	}
}

//nolint:cyclop // one sequential scenario proves both safety and explicit cleanup modes
func TestRegistryCleanRequiresExplicitPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	presence := registry.PresenceGone
	if _, err := store.Observe(context.Background(), registry.Observation{Harness: registry.HarnessCodex, Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Identity: registry.ObservationIdentity{SessionID: "gone"}, Presence: &presence, ObservedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "registry", "clean"})
	if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("unsafe clean error = %v", err)
	}
	sessions, err := store.List(context.Background(), registry.Filter{})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("unsafe clean changed registry: %v, %#v", err, sessions)
	}
	root = NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "gc"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("legacy gc without an explicit policy unexpectedly succeeded")
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "registry", "clean", "--older-than", "0s"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var cleanResult registry.GCResult
	if err := json.Unmarshal(machine.Bytes(), &cleanResult); err != nil || cleanResult.Deleted != 1 {
		t.Fatalf("registry clean JSON = %q, %v", machine.String(), err)
	}
	if _, err := store.Observe(context.Background(), registry.Observation{Harness: registry.HarnessCodex, Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Identity: registry.ObservationIdentity{SessionID: "gone-again"}, Presence: &presence, ObservedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "registry", "clean", "--all"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "deleted=1") {
		t.Fatalf("clean output = %q", stdout.String())
	}
}

func TestRegistryPathAndResetCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	observeTestSession(t, store, "reset-session", time.Now())

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "registry", "path"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout.String()) != path {
		t.Fatalf("registry path output = %q", stdout.String())
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "registry", "reset"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "cleared=1") {
		t.Fatalf("registry reset output = %q", stdout.String())
	}
}

func TestListDefaultsToNewestFirstWithUsefulLabelsAndShortIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	old := observeTestSession(t, store, "older-session", time.Now().Add(-time.Hour))
	newer := observeTestSession(t, store, "newer-session", time.Now())

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "list"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if strings.Index(output, "newer-session") > strings.Index(output, "older-session") {
		t.Fatalf("list is not newest first:\n%s", output)
	}
	if !strings.Contains(output, "SESSION") || strings.Contains(output, old.ID) || strings.Contains(output, newer.ID) {
		t.Fatalf("list did not use a label and abbreviated IDs:\n%s", output)
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "list"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sessions []registry.Session
	if err := json.Unmarshal(stdout.Bytes(), &sessions); err != nil || len(sessions) != 2 || sessions[0].ID != newer.ID {
		t.Fatalf("list JSON = %q, %v", stdout.String(), err)
	}
}

func TestAbbreviatedRegistryIDsExpandCollidingPrefixes(t *testing.T) {
	t.Parallel()
	sessions := []registry.Session{{ID: "codex-12345678aaaa"}, {ID: "codex-12345678bbbb"}, {ID: "claude-12345678cccc"}}
	ids := abbreviatedRegistryIDs(sessions)
	if ids[sessions[0].ID] != "codex-12345678a" || ids[sessions[1].ID] != "codex-12345678b" {
		t.Fatalf("colliding IDs were not expanded: %#v", ids)
	}
	if ids[sessions[2].ID] != "claude-12345678" {
		t.Fatalf("different agent prefix was unnecessarily expanded: %#v", ids)
	}
}

func TestShowResolvesShortIDAndRequiresJSONExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	session := observeTestSession(t, store, "show-session", time.Now())
	reference := shortRegistryID(session.ID)

	var human bytes.Buffer
	root := NewRootCommand(&human, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "show", reference})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "Session ID:") || !strings.Contains(human.String(), "show-session") || strings.HasPrefix(strings.TrimSpace(human.String()), "{") {
		t.Fatalf("show default output is not human-readable: %q", human.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "show", reference})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var decoded registry.Session
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil || decoded.ID != session.ID {
		t.Fatalf("show JSON = %q, %v", machine.String(), err)
	}
}

func TestStopSelectsOneSessionOrAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := registry.NewFileStore(path)
	present := true
	session, err := store.Observe(context.Background(), registry.Observation{
		Harness: registry.HarnessCodex, Source: registry.ObservationSourceProcess, Evidence: registry.ObservationEvidenceProcessPresence,
		Identity: registry.ObservationIdentity{SessionID: "stop-session"}, ProcessPresent: &present,
		Process: &registry.ProcessIdentity{PID: 4242, StartIdentity: "boot:4242"}, ObservedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "stop", shortRegistryID(session.ID), "--dry-run"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "stoppable=1") {
		t.Fatalf("single stop output = %q", stdout.String())
	}
	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "stop", "--all", "--dry-run"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["Stoppable"] != float64(1) || result["dry_run"] != true {
		t.Fatalf("stop JSON = %q, %v", stdout.String(), err)
	}

	for _, args := range [][]string{{"stop"}, {"stop", shortRegistryID(session.ID), "--all"}} {
		root = NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		root.SetArgs(append([]string{"--store", path}, args...))
		if err := root.ExecuteContext(context.Background()); err == nil {
			t.Fatalf("invalid stop selection accepted: %v", args)
		}
	}
}

//nolint:cyclop // the round trip intentionally verifies each observable state in order
func TestIntegrationsInstallStatusRemoveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv(registry.StateDirEnv, filepath.Join(home, "state"))

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"integrations", "install", "claude", "--binary", "/bin/agent-sessions"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "claude:") {
		t.Fatalf("install output = %q", stdout.String())
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"integrations", "status", "claude", "--binary", "/bin/agent-sessions"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "current") {
		t.Fatalf("status output = %q", stdout.String())
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--json", "integrations", "status", "claude", "--binary", "/bin/agent-sessions"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var statuses []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &statuses); err != nil || len(statuses) != 1 || statuses[0]["status"] != "current" {
		t.Fatalf("integration status JSON = %q, %v", stdout.String(), err)
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"integrations", "remove", "claude"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "removed") {
		t.Fatalf("remove output = %q", stdout.String())
	}
	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"integrations", "status", "claude"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "missing") {
		t.Fatalf("removed integration status = %q", stdout.String())
	}
}

func TestSetupDryRunCombinesIntegrationAndMonitor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(registry.StateDirEnv, filepath.Join(home, "state"))

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(home, "sessions.json"), "setup", "codex", "--binary", "/bin/agent-sessions", "--dry-run"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "codex:") || !strings.Contains(stdout.String(), "monitor:") || strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("setup output = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("setup dry run wrote integration: %v", err)
	}

	stdout.Reset()
	root = NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", filepath.Join(home, "sessions.json"), "--json", "setup", "codex", "--binary", "/bin/agent-sessions", "--dry-run"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result setupResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || len(result.Integrations) != 1 || result.Monitor.Manager == "" {
		t.Fatalf("setup JSON = %q, %v", stdout.String(), err)
	}
}

func TestAgentSelectionSupportsMultipleDeduplicatedAgentsAndAll(t *testing.T) {
	t.Parallel()
	selected, err := selectedHarnesses([]string{"codex", "claude", "codex"}, false)
	if err != nil || len(selected) != 2 || selected[0] != registry.HarnessCodex || selected[1] != registry.HarnessClaude {
		t.Fatalf("selected agents = %v, %v", selected, err)
	}
	selected, err = selectedHarnesses([]string{"all"}, false)
	if err != nil || len(selected) == 0 {
		t.Fatalf("all agents = %v, %v", selected, err)
	}
	if _, err := selectedHarnesses([]string{"all", "codex"}, false); !errors.Is(err, errAllWithAgents) {
		t.Fatalf("mixed all selection error = %v", err)
	}
}

func TestMonitorLifecycleCommandsUseHumanOutputUnlessJSONRequested(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(registry.StateDirEnv, filepath.Join(home, "state"))
	storePath := filepath.Join(home, "sessions.json")

	for _, args := range [][]string{{"monitor", "enable", "--dry-run"}, {"monitor", "status"}, {"monitor", "disable", "--dry-run"}} {
		var stdout bytes.Buffer
		root := NewRootCommand(&stdout, &bytes.Buffer{})
		root.SetArgs(append([]string{"--store", storePath}, args...))
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v failed: %v", args, err)
		}
		if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "manager=") {
			t.Fatalf("%v default output = %q", args, stdout.String())
		}
	}

	var stdout bytes.Buffer
	root := NewRootCommand(&stdout, &bytes.Buffer{})
	root.SetArgs([]string{"--store", storePath, "--json", "monitor", "enable", "--dry-run"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result service.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Manager == "" {
		t.Fatalf("monitor JSON = %q, %v", stdout.String(), err)
	}
}

func TestMonitorRunOnceSupportsHumanAndJSONOutput(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	var human bytes.Buffer
	root := NewRootCommand(&human, &bytes.Buffer{})
	root.SetArgs([]string{"--store", storePath, "monitor", "run", "--once"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(human.String()), "{") || !strings.Contains(human.String(), "processes=") {
		t.Fatalf("monitor run human output = %q", human.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", storePath, "--json", "monitor", "run", "--once"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(machine.Bytes(), &result); err != nil {
		t.Fatalf("monitor run JSON = %q, %v", machine.String(), err)
	}
}

func TestDoctorIsConciseUnlessVerbose(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(registry.StateDirEnv, filepath.Join(home, "state"))
	path := filepath.Join(home, "sessions.json")

	var concise bytes.Buffer
	root := NewRootCommand(&concise, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "doctor"})
	_ = root.ExecuteContext(context.Background())
	if strings.Contains(concise.String(), "SESSION_START") || strings.Contains(concise.String(), "integration.codex") {
		t.Fatalf("concise doctor contains full matrix:\n%s", concise.String())
	}
	installRoot := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	installRoot.SetArgs([]string{"integrations", "install", "codex", "--binary", defaultInstallBinary()})
	if err := installRoot.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	concise.Reset()
	root = NewRootCommand(&concise, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "doctor"})
	_ = root.ExecuteContext(context.Background())
	if !strings.Contains(concise.String(), "integration.codex") || strings.Contains(concise.String(), "SESSION_START") {
		t.Fatalf("concise doctor omitted installed integration or added matrix:\n%s", concise.String())
	}

	var verbose bytes.Buffer
	root = NewRootCommand(&verbose, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "doctor", "--verbose"})
	_ = root.ExecuteContext(context.Background())
	if !strings.Contains(verbose.String(), "SESSION_START") || !strings.Contains(verbose.String(), "integration.codex") {
		t.Fatalf("verbose doctor omitted details:\n%s", verbose.String())
	}

	var machine bytes.Buffer
	root = NewRootCommand(&machine, &bytes.Buffer{})
	root.SetArgs([]string{"--store", path, "--json", "doctor"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result doctorResult
	if err := json.Unmarshal(machine.Bytes(), &result); err != nil || len(result.Capabilities) != 0 {
		t.Fatalf("concise doctor JSON = %q, %v", machine.String(), err)
	}
}

func observeTestSession(t *testing.T, store registry.Store, sessionID string, at time.Time) registry.Session {
	t.Helper()
	activity := registry.ActivityIdle
	session, err := store.Observe(context.Background(), registry.Observation{Harness: registry.HarnessCodex, Source: registry.ObservationSourceNative, Evidence: registry.ObservationEvidenceNativeEvent, Identity: registry.ObservationIdentity{SessionID: sessionID}, Activity: &activity, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return session
}
