package tmuxctx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const defaultCaptureLines = 100

var (
	errMissingCapturePane    = errors.New("capture pane id is required")
	errInvalidServerIdentity = errors.New("invalid tmux server identity")
)

type ScreenSnapshot struct {
	Text  string
	Title string
}

type CaptureOptions struct {
	Env   Env
	Run   CommandRunner
	Lines int
}

func CapturePane(ctx context.Context, pane Pane) (ScreenSnapshot, error) {
	return CapturePaneWithOptions(ctx, pane, CaptureOptions{Env: Env{TMUX: "", TMUXPane: ""}, Run: nil, Lines: 0})
}

func CapturePaneWithOptions(ctx context.Context, pane Pane, options CaptureOptions) (ScreenSnapshot, error) {
	if strings.TrimSpace(pane.Tmux.PaneID) == "" {
		return ScreenSnapshot{}, errMissingCapturePane
	}
	run := options.Run
	if run == nil {
		run = runTmuxWithEnv
	}
	lines := options.Lines
	if lines <= 0 || lines > defaultCaptureLines {
		lines = defaultCaptureLines
	}
	serverArgs, err := serverArgsForIdentity(pane.ServerIdentity)
	if err != nil {
		return ScreenSnapshot{}, err
	}
	captureArgs := append(append([]string{}, serverArgs...), "capture-pane", "-p", "-J", "-e", "-S", "-"+strconv.Itoa(lines), "-t", pane.Tmux.PaneID)
	text, err := run(ctx, options.Env, captureArgs...)
	if err != nil {
		return ScreenSnapshot{}, fmt.Errorf("capturing pane %s: %w", pane.Tmux.PaneID, err)
	}
	titleArgs := append(append([]string{}, serverArgs...), "display-message", "-p", "-t", pane.Tmux.PaneID, "-F", "#{pane_title}")
	title, titleErr := run(ctx, options.Env, titleArgs...)
	if titleErr != nil {
		title = ""
	}
	return ScreenSnapshot{Text: boundBottomLines(text, lines), Title: strings.TrimRight(title, "\r\n")}, nil
}

func boundBottomLines(text string, limit int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n")
}

func serverArgsForIdentity(identity string) ([]string, error) {
	identity = strings.TrimSpace(identity)
	switch {
	case identity == "", identity == "default":
		return nil, nil
	case strings.HasPrefix(identity, "-L:"):
		name := strings.TrimPrefix(identity, "-L:")
		if name == "" {
			return nil, fmt.Errorf("%w: %q", errInvalidServerIdentity, identity)
		}
		return []string{"-L", name}, nil
	default:
		return []string{"-S", identity}, nil
	}
}
