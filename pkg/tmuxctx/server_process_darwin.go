//go:build darwin

package tmuxctx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func listCurrentUserTmuxServers(ctx context.Context) ([]ServerProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processTable, err := unix.SysctlKinfoProcSlice("kern.proc.uid", unix.Getuid())
	if err != nil {
		return nil, fmt.Errorf("listing tmux processes: %w", err)
	}
	processes := make([]ServerProcess, 0)
	for _, process := range processTable {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid := int(process.Proc.P_pid)
		command := darwinProcessCommand(process.Proc.P_comm[:])
		if pid <= 0 || !strings.HasPrefix(filepath.Base(command), "tmux") {
			continue
		}
		data, readErr := unix.SysctlRaw("kern.procargs2", pid)
		if readErr != nil {
			if errors.Is(readErr, unix.ESRCH) || errors.Is(readErr, unix.EPERM) || errors.Is(readErr, unix.EACCES) {
				continue
			}
			return nil, fmt.Errorf("reading tmux process %d arguments: %w", pid, readErr)
		}
		args, parseErr := parseDarwinProcArgs(data)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing tmux process %d arguments: %w", pid, parseErr)
		}
		if strings.HasPrefix(command, "tmux: server") || isTmuxServerArgs(args) {
			processes = append(processes, ServerProcess{PID: pid, Args: args})
		}
	}

	return processes, nil
}

func darwinProcessCommand(data []byte) string {
	if index := bytes.IndexByte(data, 0); index >= 0 {
		data = data[:index]
	}

	return string(data)
}
