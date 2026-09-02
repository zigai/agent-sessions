package harness

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (writer *cappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := writer.limit - writer.buffer.Len()
	if remaining > 0 {
		_, _ = writer.buffer.Write(data[:min(len(data), remaining)])
	}
	return written, nil
}

func (writer *cappedBuffer) Bytes() []byte  { return writer.buffer.Bytes() }
func (writer *cappedBuffer) String() string { return writer.buffer.String() }

// RunCommand executes a command with bounded output and a caller-specified timeout.
func RunCommand(parent context.Context, timeout time.Duration, outputLimit int, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	stdout := cappedBuffer{buffer: bytes.Buffer{}, limit: outputLimit}
	stderr := cappedBuffer{buffer: bytes.Buffer{}, limit: outputLimit}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s timed out: %w", command, ctx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return nil, fmt.Errorf("%s %s: %w", command, detail, err)
		}
		return nil, fmt.Errorf("%s: %w", command, err)
	}
	return stdout.Bytes(), nil
}
