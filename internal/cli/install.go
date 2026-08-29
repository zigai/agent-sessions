package cli

import (
	"os"
	"path/filepath"
)

func defaultInstallBinary() string {
	executable, err := os.Executable()
	if err != nil {
		return "aht"
	}

	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil && resolved != "" {
		executable = resolved
	}

	absolute, err := filepath.Abs(executable)
	if err != nil {
		return executable
	}

	return absolute
}
