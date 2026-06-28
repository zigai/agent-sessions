//go:build darwin

package processinfo

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// StartIdentity returns a stable process start identity for pid on macOS.
func StartIdentity(ctx context.Context, pid int) string {
	if pid <= 0 {
		return ""
	}

	output, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// CommandName returns the executable command name for pid on macOS.
func CommandName(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", nil
	}

	output, err := exec.CommandContext(ctx, "ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("checking context: %w", ctxErr)
		}

		return "", nil
	}

	return strings.TrimSpace(string(output)), nil
}
