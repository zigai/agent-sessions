//go:build linux

package processinfo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// StartIdentity returns a stable process start identity for pid on Linux.
func StartIdentity(ctx context.Context, pid int) string {
	if pid <= 0 || ctx.Err() != nil {
		return ""
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}

	return linuxStartTimeFromStat(string(data))
}

// CommandName returns the executable command name for pid on Linux.
func CommandName(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("checking context: %w", err)
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("reading process command: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

func linuxStartTimeFromStat(stat string) string {
	closeParen := strings.LastIndexByte(stat, ')')
	if closeParen < 0 || closeParen+2 >= len(stat) {
		return ""
	}
	fields := strings.Fields(stat[closeParen+2:])
	const startTimeFieldIndexAfterComm = 19
	if len(fields) <= startTimeFieldIndexAfterComm {
		return ""
	}

	return fields[startTimeFieldIndexAfterComm]
}
