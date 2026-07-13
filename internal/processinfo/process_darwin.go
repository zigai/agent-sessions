//go:build darwin

package processinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// List returns a current-user process snapshot from the kernel process table,
// enriched by one ps and one lsof invocation.
func List(ctx context.Context) ([]Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kinfo, err := unix.SysctlKinfoProcSlice("kern.proc.uid", unix.Getuid())
	if err != nil {
		return nil, classifyDarwinError("kern.proc.uid", err)
	}
	if kinfo == nil {
		return nil, &TableError{Path: "kern.proc.uid", Err: errors.New("nil process table")}
	}

	pids := make([]string, 0, len(kinfo))
	processes := make([]Process, 0, len(kinfo))
	for _, process := range kinfo {
		pid := int(process.Proc.P_pid)
		if pid <= 0 || process.Eproc.Ppid < 0 || process.Eproc.Pgid < 0 || process.Proc.P_starttime.Sec <= 0 || process.Proc.P_starttime.Usec < 0 || process.Proc.P_starttime.Usec >= 1_000_000 {
			return nil, &TableError{Path: "kern.proc.uid", Err: fmt.Errorf("invalid process record pid=%d", pid)}
		}
		pids = append(pids, strconv.Itoa(pid))
		processes = append(processes, Process{
			PID:            pid,
			PPID:           int(process.Eproc.Ppid),
			ProcessGroupID: int(process.Eproc.Pgid),
			StartIdentity:  fmt.Sprintf("%d:%06d", process.Proc.P_starttime.Sec, process.Proc.P_starttime.Usec),
			Executable:     darwinCString(process.Proc.P_comm[:]),
		})
	}
	if len(pids) == 0 {
		return processes, nil
	}

	psOutput, err := exec.CommandContext(ctx, "/bin/ps", "-o", "pid=,ppid=,pgid=,tty=,comm=,args=", "-p", strings.Join(pids, ",")).Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, classifyDarwinError("/bin/ps", err)
	}
	byPID, err := parseDarwinPS(string(psOutput))
	if err != nil {
		return nil, &TableError{Path: "/bin/ps", Err: err}
	}
	for i := range processes {
		row, ok := byPID[processes[i].PID]
		if !ok {
			continue
		}
		processes[i].PPID = row.PPID
		processes[i].ProcessGroupID = row.ProcessGroupID
		if row.Executable != "" {
			processes[i].Executable = row.Executable
		}
		processes[i].TTY = row.TTY
		processes[i].Args = row.Args
	}

	if cwd, err := darwinLsofCWD(ctx, pids); err == nil {
		for i := range processes {
			processes[i].CWD = cwd[processes[i].PID]
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return processes, nil
}

type darwinPSRow struct {
	PID            int
	PPID           int
	ProcessGroupID int
	TTY            string
	Executable     string
	Args           []string
}

func parseDarwinPS(output string) (map[int]darwinPSRow, error) {
	rows := make(map[int]darwinPSRow)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil, errors.New("truncated ps record")
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			return nil, errors.New("invalid ps pid")
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid < 0 {
			return nil, errors.New("invalid ps parent pid")
		}
		pgid, err := strconv.Atoi(fields[2])
		if err != nil || pgid < 0 {
			return nil, errors.New("invalid ps process group id")
		}
		args := append([]string(nil), fields[5:]...)
		rows[pid] = darwinPSRow{PID: pid, PPID: ppid, ProcessGroupID: pgid, TTY: fields[3], Executable: fields[4], Args: args}
	}
	return rows, nil
}

func darwinLsofCWD(ctx context.Context, pids []string) (map[int]string, error) {
	output, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-d", "cwd", "-p", strings.Join(pids, ","), "-Fn").Output()
	if err != nil {
		return nil, err
	}
	cwds := make(map[int]string)
	pid := 0
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			value, err := strconv.Atoi(line[1:])
			if err != nil || value <= 0 {
				return nil, errors.New("invalid lsof pid")
			}
			pid = value
		case 'n':
			if pid > 0 {
				cwds[pid] = line[1:]
			}
		}
	}
	return cwds, nil
}

func darwinCString(data []byte) string {
	if index := bytes.IndexByte(data, 0); index >= 0 {
		data = data[:index]
	}
	return string(data)
}

func classifyDarwinError(path string, err error) error {
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return &PermissionError{Path: path, Err: err}
	}
	return fmt.Errorf("reading process information at %s: %w", path, err)
}

// StartIdentity returns the process start identity reported by ps.
func StartIdentity(ctx context.Context, pid int) string {
	if pid <= 0 {
		return ""
	}
	output, err := exec.CommandContext(ctx, "/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
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
	output, err := exec.CommandContext(ctx, "/bin/ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("checking context: %w", ctxErr)
		}
		return "", nil
	}
	return strings.TrimSpace(string(output)), nil
}
