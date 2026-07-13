//go:build darwin

package tmuxctx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func listCurrentUserTmuxServers(ctx context.Context) ([]ServerProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	uid := strconv.Itoa(os.Getuid())
	output, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,uid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("listing tmux processes: %w", err)
	}
	processes := make([]ServerProcess, 0)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != uid {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil {
			continue
		}
		args := fields[2:]
		if isTmuxServerArgs(args) {
			processes = append(processes, ServerProcess{PID: pid, Args: args})
		}
	}
	return processes, nil
}
