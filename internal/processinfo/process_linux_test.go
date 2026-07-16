//go:build linux

package processinfo

import (
	"os"
	"strings"
	"testing"
)

func TestParseLinuxStatHandlesParentheses(t *testing.T) {
	fields := []string{"S", "41", "42"}
	for len(fields) < 19 {
		fields = append(fields, "0")
	}
	fields[5] = "42"
	fields = append(fields, "987654")
	got, err := parseLinuxStat("123 (agent ) worker) " + strings.Join(fields, " "))
	if err != nil {
		t.Fatalf("parseLinuxStat returned error: %v", err)
	}
	if got.PID != 123 || got.PPID != 41 || got.ProcessGroupID != 42 || !got.Foreground || got.StartIdentity != "987654" {
		t.Fatalf("parsed process = %#v", got)
	}
}

func TestLinuxEnvironmentValueFindsScopedAgentHint(t *testing.T) {
	t.Parallel()
	got := linuxEnvironmentValue([]byte("PATH=/usr/bin\x00AGENT_SESSIONS_AGENT=codex\x00OTHER=value\x00"), "AGENT_SESSIONS_AGENT")
	if got != "codex" {
		t.Fatalf("linuxEnvironmentValue = %q, want codex", got)
	}
}

func TestParseLinuxStatRejectsMalformedRecord(t *testing.T) {
	if _, err := parseLinuxStat("123 (agent) S 1"); err == nil {
		t.Fatal("expected malformed stat record error")
	}
}

func TestListCurrentUser(t *testing.T) {
	processes, err := List(t.Context())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	var current *Process
	for i := range processes {
		if processes[i].PID == os.Getpid() {
			current = &processes[i]
			break
		}
	}
	if current == nil {
		t.Fatal("List did not include the current process")
	}
	if current.StartIdentity == "" || !strings.Contains(current.StartIdentity, ":") {
		t.Fatalf("current process identity = %q", current.StartIdentity)
	}
}
