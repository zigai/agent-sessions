package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

const benchmarkClaudePayload = `{"session_id":"bench-claude","transcript_path":"/tmp/.claude/projects/bench.jsonl","cwd":"/tmp","hook_event_name":"UserPromptSubmit"}`

func BenchmarkReportSyncNoTmux(b *testing.B) {
	app := &application{storePath: filepath.Join(b.TempDir(), "state.json"), stdout: io.Discard, stderr: io.Discard}
	options := benchmarkReportOptions(false)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := app.runReport(ctx, strings.NewReader(benchmarkClaudePayload), options); err != nil {
			b.Fatalf("runReport() error = %v", err)
		}
	}
}

func BenchmarkReportQueueEnqueue(b *testing.B) {
	app := &application{storePath: filepath.Join(b.TempDir(), "state.json"), stdout: io.Discard, stderr: io.Discard}
	options := benchmarkReportOptions(true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := app.runReport(ctx, strings.NewReader(benchmarkClaudePayload), options); err != nil {
			b.Fatalf("runReport() error = %v", err)
		}
	}
}

func benchmarkReportOptions(queue bool) reportOptions {
	return reportOptions{
		harness:         "claude",
		state:           "running",
		source:          "claude-hook",
		attributes:      []string{"agent_sessions_integration=claude-hook"},
		rawStdin:        true,
		noTmux:          true,
		queue:           queue,
		quiet:           true,
		cwdAuto:         true,
		projectRootAuto: true,
	}
}
