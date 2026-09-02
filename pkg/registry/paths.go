package registry

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// StorePathEnv overrides the exact registry store path.
	StorePathEnv = "AHT_STORE"
	// StateDirEnv overrides the registry state directory.
	StateDirEnv = "AHT_STATE_DIR"
)

func DefaultStateDir() string {
	if value := stringsTrimmedEnv(StateDirEnv); value != "" {
		return value
	}

	if value := stringsTrimmedEnv("XDG_STATE_HOME"); value != "" {
		return filepath.Join(value, "aht")
	}

	if home := stringsTrimmedEnv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "aht")
	}

	return filepath.Join(os.TempDir(), "aht")
}

func DefaultStorePath() string {
	if value := stringsTrimmedEnv(StorePathEnv); value != "" {
		return value
	}

	return filepath.Join(DefaultStateDir(), "state.json")
}

func stringsTrimmedEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}
