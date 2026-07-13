//go:build linux

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var errInactive = errors.New("inactive")

type recordingExecutor struct {
	calls [][]string
	err   error
}

func (r *recordingExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if r.err != nil {
		return nil, r.err
	}
	return nil, nil
}

func TestRenderSystemdUnit(t *testing.T) {
	got, err := RenderSystemdUnit(Options{Binary: "/tmp/agent sessions", StorePath: "/tmp/state.json", Interval: 3 * time.Second, GracePeriod: 0})
	if err != nil {
		t.Fatal(err)
	}
	want := "# agent-sessions managed observer service\n# version: 2\n[Unit]\nDescription=Agent Sessions observer\n\n[Service]\nExecStart=\"/tmp/agent sessions\" --store /tmp/state.json observe --interval 3s --grace-period 0s --quiet\nRestart=on-failure\n\n[Install]\nWantedBy=default.target\n"
	if got != want {
		t.Fatalf("rendered unit = %q, want %q", got, want)
	}
}

func TestInstallUsesAtomicContentAndManagerArgv(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	executor := &recordingExecutor{}
	options := Options{Binary: "agent-sessions", StorePath: filepath.Join(config, "store.json"), Interval: 3 * time.Second}
	result, err := New(executor).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Current || !result.Installed {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(config, "systemd", "user", linuxUnitName))
	if err != nil {
		t.Fatal(err)
	}
	if !isManaged(string(content)) {
		t.Fatal("managed marker missing")
	}
	wantCalls := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", linuxUnitName},
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", executor.calls, wantCalls)
	}
}

func TestInstallDryRunAndForeignRefusal(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	path := filepath.Join(config, "systemd", "user", linuxUnitName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(&recordingExecutor{}).Update(context.Background(), Options{Binary: "/bin/agent-sessions", StorePath: "/tmp/store"})
	if !errors.Is(err, ErrForeign) {
		t.Fatalf("error = %v, want ErrForeign", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{}
	result, err := New(executor).Install(context.Background(), Options{Binary: "/bin/agent-sessions", StorePath: "/tmp/store", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Installed {
		t.Fatalf("dry-run mutated result: %+v", result)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("dry-run manager calls = %#v", executor.calls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run path error = %v", err)
	}
}

func TestStatusManagerStoppedIsRepresented(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	executor := &recordingExecutor{}
	options := Options{Binary: "/bin/agent-sessions", StorePath: "/tmp/store"}
	if _, err := New(executor).Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	executor.err = errInactive
	result, err := New(executor).Status(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Running {
		t.Fatalf("stopped service marked running: %+v", result)
	}
	if result.Message != "not running" {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestUpdateRestartsManagedService(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	options := Options{Binary: "/bin/agent-sessions", StorePath: "/tmp/store"}
	if err := os.MkdirAll(filepath.Join(config, "systemd", "user"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config, "systemd", "user", linuxUnitName)
	if err := os.WriteFile(path, []byte("# "+managedMarker+"\n# version: 2\nstale"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &recordingExecutor{}
	result, err := New(executor).Update(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Message != "updated" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(executor.calls) != 2 || executor.calls[1][2] != "restart" {
		t.Fatalf("calls = %#v, want daemon-reload and restart", executor.calls)
	}
}
