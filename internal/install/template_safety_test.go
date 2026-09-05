//go:build integration

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zigai/aht/pkg/registry"
)

func TestTypeScriptReportingOwnsProcesses(t *testing.T) {
	for _, harness := range []registry.Harness{
		registry.HarnessPi, registry.HarnessOmp, registry.HarnessOpenCode, registry.HarnessKilo,
	} {
		t.Run(string(harness), func(t *testing.T) {
			command, capture := stalledReportingBinary(t)
			var module string
			for _, artifact := range collectGeneratedArtifacts(t, captureExecutable{command: command, path: capture}) {
				if artifact.harness == harness && strings.HasSuffix(artifact.path, "aht-state.ts") {
					module = artifact.content
					break
				}
			}
			if module == "" {
				t.Fatal("missing generated extension")
			}
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "package.json"), `{"type":"module"}`, 0o600)
			writeTestFile(t, filepath.Join(dir, "extension.ts"), module, 0o600)
			driver := `
import { createInterface } from "node:readline";
const lines = createInterface({input: process.stdin})[Symbol.asyncIterator]();
import extension from "./extension.ts";
const hooks = new Map();
extension({on: (name, handler) => hooks.set(name, handler)});
const ctx = {hasUI: true, cwd: "/project", sessionManager: {
  getSessionId: () => "owned-session", getSessionFile: () => "/project/session.jsonl", getBranch: () => [],
}};
hooks.get("session_start")({type: "session_start"}, ctx);
await lines.next();
for (let index = 0; index < 100; index++) {
  hooks.get("session_start")({type: "session_start"}, ctx);
}
await hooks.get("session_shutdown")({type: "session_shutdown"}, ctx);
await lines.next();
process.stdin.destroy();
`
			terminalEvent := "session_shutdown"
			if harness == registry.HarnessOpenCode || harness == registry.HarnessKilo {
				driver = `
import { createInterface } from "node:readline";
const lines = createInterface({input: process.stdin})[Symbol.asyncIterator]();
import plugin from "./extension.ts";
const hooks = await plugin.server({directory: "/project", worktree: "/project"});
const sends = [hooks.event({event: {type: "session.created", sessionID: "owned-session"}})];
await lines.next();
for (let index = 0; index < 100; index++) {
  sends.push(hooks.event({event: {type: "session.status", properties: {sessionID: "owned-session", status: {type: "busy"}}}}));
}
sends.push(hooks.event({event: {type: "session.deleted", sessionID: "owned-session"}}));
await Promise.all(sends);
await lines.next();
process.stdin.destroy();
`
				terminalEvent = "session.deleted"
			}
			runReportingDriver(t, "node", dir, driver, capture, terminalEvent)
			records := readReportingRecords(t, capture)
			if len(records) < 2 || len(records) > 65 {
				t.Fatalf("executed %d reports, want initial plus bounded pending FIFO", len(records))
			}
			for _, record := range records {
				if record.Overlap {
					t.Fatal("report started before previous subprocess was reaped")
				}
				if !strings.Contains(strings.Join(record.Args, "\n"), "owned-session") {
					t.Fatalf("report lost session identity: %v", record.Args)
				}
			}
			if !strings.Contains(strings.Join(records[len(records)-1].Args, "\n"), terminalEvent) {
				t.Fatalf("final report was not retained: %v", records[len(records)-1].Args)
			}
		})
	}
}

func TestTypeScriptReportingRejectsMalformedEventFields(t *testing.T) {
	capture := captureBinary(t)
	t.Setenv("AHT_CAPTURE", capture.path)
	for _, harness := range []registry.Harness{registry.HarnessPi, registry.HarnessOmp} {
		t.Run(string(harness), func(t *testing.T) {
			module := generatedArtifactContent(t, harness, "aht-state.ts")
			runNodeRuntime(t, "extension.ts", module, `
import extension from "./extension.ts";
const hooks = new Map();
extension({on: (name, callback) => hooks.set(name, callback)});
const ctx = {hasUI: true, sessionManager: {getBranch: () => [], getSessionId: () => "boundary-session"}};
hooks.get("session_start")(null, ctx);
hooks.get("agent_start")({type: ["not-a-string"]}, ctx);
hooks.get("agent_end")({messages: [null, 7, {role: "assistant", stopReason: "aborted"}], error: {secret: "DO_NOT_REPORT"}}, ctx);
await hooks.get("session_shutdown")({type: "session_shutdown"}, ctx);
`, nil)
		})
	}
	data, err := os.ReadFile(capture.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "DO_NOT_REPORT") || strings.Contains(string(data), "not-a-string") {
		t.Fatalf("malformed or sensitive fields escaped projection: %s", data)
	}
	if !strings.Contains(string(data), "interrupted\n") {
		t.Fatalf("valid assistant interruption was lost: %s", data)
	}
}

func TestTypeScriptReportingMissingExecutableIsNonfatal(t *testing.T) {
	for _, harness := range []registry.Harness{
		registry.HarnessPi, registry.HarnessOmp, registry.HarnessOpenCode, registry.HarnessKilo,
	} {
		t.Run(string(harness), func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "missing-reporter")
			for _, artifact := range collectGeneratedArtifacts(t, captureExecutable{command: binary}) {
				if artifact.harness != harness || !strings.HasSuffix(artifact.path, "aht-state.ts") {
					continue
				}
				driver := `
import extension from "./extension.ts";
const hooks = new Map();
extension({on: (name, handler) => hooks.set(name, handler)});
const ctx = {hasUI: true, sessionManager: {getBranch: () => [], getSessionId: () => "missing-binary"}};
hooks.get("session_start")({type: "session_start"}, ctx);
await hooks.get("session_shutdown")({type: "session_shutdown"}, ctx);
`
				if harness == registry.HarnessOpenCode || harness == registry.HarnessKilo {
					driver = `
import plugin from "./extension.ts";
const hooks = await plugin.server({});
await hooks.event({event: {type: "session.created", sessionID: "missing-binary"}});
`
				}
				runNodeRuntime(t, "extension.ts", artifact.content, driver, nil)
				return
			}
			t.Fatal("missing generated extension")
		})
	}
}
