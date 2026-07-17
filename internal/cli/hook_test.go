package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedHookRequiresExplicitJSON(t *testing.T) {
	t.Parallel()
	app := &application{storePath: filepath.Join(t.TempDir(), "sessions.json"), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := app.runManagedHook(context.Background(), strings.NewReader(`{"conversationId":"session-1"}`), "agy", managedHookOptions{event: "PreInvocation"})
	if !errors.Is(err, errManagedHookJSONRequired) {
		t.Fatalf("error = %v, want %v", err, errManagedHookJSONRequired)
	}
}

func TestManagedHookEmitsProtocolJSONWhenRequested(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	app := &application{storePath: filepath.Join(t.TempDir(), "sessions.json"), outputJSON: true, stdout: &stdout, stderr: &bytes.Buffer{}}
	payload := `{"conversationId":"session-1","workspacePaths":["/repo"],"transcriptPath":"/tmp/transcript.jsonl","invocationNum":0,"initialNumSteps":0}`
	if err := app.runManagedHook(context.Background(), strings.NewReader(payload), "agy", managedHookOptions{event: "PreInvocation"}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("expected hook protocol JSON: %v; output=%q", err, stdout.String())
	}
}

func TestManagedHookGeneratedCommandUsesFastPath(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	payload := `{"conversationId":"session-1","workspacePaths":["/repo"],"transcriptPath":"/tmp/transcript.jsonl","invocationNum":0,"initialNumSteps":0}`
	handled, err := tryExecuteFastPath(
		context.Background(),
		[]string{"--store", filepath.Join(t.TempDir(), "sessions.json"), "--json", "hook", "agy", "--event", "PreInvocation", "--queue"},
		strings.NewReader(payload),
		&stdout,
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("generated hook command did not use fast path")
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("expected protocol JSON, got %q", stdout.String())
	}
}

func TestManagedHookRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	app := &application{storePath: filepath.Join(t.TempDir(), "sessions.json"), outputJSON: true, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := app.runManagedHook(
		context.Background(),
		strings.NewReader(strings.Repeat("x", maxPayloadInputBytes+1)),
		"agy",
		managedHookOptions{event: "PreInvocation"},
	)
	if !errors.Is(err, errPayloadInputTooLarge) {
		t.Fatalf("error = %v, want %v", err, errPayloadInputTooLarge)
	}
}
