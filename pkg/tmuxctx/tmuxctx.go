package tmuxctx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const fieldSeparator = "\t"

var (
	// ErrNoTmuxContext is returned when no tmux environment is available.
	ErrNoTmuxContext = errors.New("not inside tmux")
	// ErrInvalidFieldCount is returned when tmux output does not match the requested format.
	ErrInvalidFieldCount = errors.New("invalid tmux field count")
	errMissingTmuxPaneID = errors.New("missing tmux pane id")
)

type Pane struct {
	Tmux           registry.TmuxContext
	CurrentCommand string
}

func Current(ctx context.Context) (registry.TmuxContext, error) {
	if os.Getenv("TMUX") == "" && os.Getenv("TMUX_PANE") == "" {
		return registry.TmuxContext{}, ErrNoTmuxContext
	}

	format := strings.Join([]string{
		"#{session_id}",
		"#{session_name}",
		"#{window_id}",
		"#{window_index}",
		"#{window_name}",
		"#{pane_id}",
		"#{pane_index}",
		"#{pane_current_path}",
		"#{pane_pid}",
		"#{pane_tty}",
		"#{client_tty}",
	}, fieldSeparator)
	output, err := runTmux(ctx, currentDisplayMessageArgs(format, os.Getenv("TMUX_PANE"))...)
	if err != nil {
		if paneID := os.Getenv("TMUX_PANE"); paneID != "" {
			return registry.TmuxContext{
				Inside:          true,
				SessionID:       "",
				SessionName:     "",
				WindowID:        "",
				WindowIndex:     "",
				WindowName:      "",
				PaneID:          paneID,
				PaneIndex:       "",
				PaneCurrentPath: "",
				PanePID:         0,
				PaneTTY:         "",
				ClientTTY:       "",
			}, nil
		}

		return registry.TmuxContext{}, err
	}

	return ParseCurrent(output)
}

func currentDisplayMessageArgs(format string, paneID string) []string {
	args := []string{"display-message", "-p"}
	if paneID != "" {
		args = append(args, "-t", paneID)
	}

	return append(args, "-F", format)
}

func ListPanes(ctx context.Context) ([]Pane, error) {
	format := strings.Join([]string{
		"#{session_id}",
		"#{session_name}",
		"#{window_id}",
		"#{window_index}",
		"#{window_name}",
		"#{pane_id}",
		"#{pane_index}",
		"#{pane_current_path}",
		"#{pane_pid}",
		"#{pane_tty}",
		"#{pane_current_command}",
	}, fieldSeparator)
	output, err := runTmux(ctx, "list-panes", "-a", "-F", format)
	if err != nil {
		return nil, err
	}

	return ParseListPanes(output)
}

func SendInterrupt(ctx context.Context, paneID string) error {
	if strings.TrimSpace(paneID) == "" {
		return errMissingTmuxPaneID
	}
	_, err := runTmux(ctx, "send-keys", "-t", paneID, "C-c")
	if err != nil {
		return err
	}

	return nil
}

func ParseCurrent(output string) (registry.TmuxContext, error) {
	fields := splitFields(output)
	const expectedFields = 11
	if len(fields) != expectedFields {
		return registry.TmuxContext{}, fmt.Errorf("%w: expected %d, got %d", ErrInvalidFieldCount, expectedFields, len(fields))
	}

	return registry.TmuxContext{
		Inside:          true,
		SessionID:       fields[0],
		SessionName:     fields[1],
		WindowID:        fields[2],
		WindowIndex:     fields[3],
		WindowName:      fields[4],
		PaneID:          fields[5],
		PaneIndex:       fields[6],
		PaneCurrentPath: fields[7],
		PanePID:         parsePositiveInt(fields[8]),
		PaneTTY:         fields[9],
		ClientTTY:       fields[10],
	}, nil
}

func ParseListPanes(output string) ([]Pane, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}

	panes := make([]Pane, 0, len(lines))
	for _, line := range lines {
		fields := splitFields(line)
		const expectedFields = 11
		if len(fields) != expectedFields {
			return nil, fmt.Errorf("%w: expected %d, got %d", ErrInvalidFieldCount, expectedFields, len(fields))
		}

		panes = append(panes, Pane{
			Tmux: registry.TmuxContext{
				Inside:          true,
				SessionID:       fields[0],
				SessionName:     fields[1],
				WindowID:        fields[2],
				WindowIndex:     fields[3],
				WindowName:      fields[4],
				PaneID:          fields[5],
				PaneIndex:       fields[6],
				PaneCurrentPath: fields[7],
				PanePID:         parsePositiveInt(fields[8]),
				PaneTTY:         fields[9],
				ClientTTY:       "",
			},
			CurrentCommand: fields[10],
		})
	}

	return panes, nil
}

func splitFields(output string) []string {
	return strings.Split(strings.TrimRight(output, "\r\n"), fieldSeparator)
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}

	return parsed
}

func runTmux(ctx context.Context, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("running tmux %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}
