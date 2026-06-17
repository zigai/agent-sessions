package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// StorePathEnv overrides the exact registry store path.
	StorePathEnv = "AGENT_SESSIONS_STORE"
	// StateDirEnv overrides the registry state directory.
	StateDirEnv = "AGENT_SESSIONS_STATE_DIR"
)

func DefaultStateDir() string {
	if value := stringsTrimmedEnv(StateDirEnv); value != "" {
		return value
	}

	if value := stringsTrimmedEnv("XDG_STATE_HOME"); value != "" {
		return filepath.Join(value, "agent-sessions")
	}

	if runtime.GOOS == "windows" {
		if value := stringsTrimmedEnv("LOCALAPPDATA"); value != "" {
			return filepath.Join(value, "agent-sessions")
		}
	}

	if home := stringsTrimmedEnv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "agent-sessions")
	}

	return filepath.Join(os.TempDir(), "agent-sessions")
}

func DefaultStorePath() string {
	if value := stringsTrimmedEnv(StorePathEnv); value != "" {
		return value
	}

	return filepath.Join(DefaultStateDir(), "state.json")
}

func stringsTrimmedEnv(name string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}
