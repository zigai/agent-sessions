//go:build linux || darwin

package reportqueue

import (
	"fmt"
	"syscall"
)

func syscallKillZero(pid int) error {
	if err := syscall.Kill(pid, 0); err != nil {
		return fmt.Errorf("checking process %d: %w", pid, err)
	}

	return nil
}
