package tmux

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zigai/aht/pkg/mux"
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
	lines := min(options.Lines, defaultCaptureLines)
	serverArgs, err := serverArgsForIdentity(pane.ServerIdentity)
	if err != nil {
		return ScreenSnapshot{}, err
	}
	captureArgs := append(append([]string{}, serverArgs...), "capture-pane", "-p", "-J", "-e")
	if lines > 0 {
		captureArgs = append(captureArgs, "-S", "-"+strconv.Itoa(lines))
	}
	captureArgs = append(captureArgs, "-t", pane.Tmux.PaneID)
	text, err := run(ctx, options.Env, captureArgs...)
	if err != nil {
		return ScreenSnapshot{}, fmt.Errorf("capturing pane %s: %w", pane.Tmux.PaneID, err)
	}
	titleArgs := append(append([]string{}, serverArgs...), "display-message", "-p", "-t", pane.Tmux.PaneID, "-F", "#{pane_title}")
	title, titleErr := run(ctx, options.Env, titleArgs...)
	if titleErr != nil {
		title = ""
	}
	if lines > 0 {
		text = strings.Join(mux.BoundBottomLines(text, lines), "\n")
	}
	return ScreenSnapshot{Text: text, Title: strings.TrimRight(title, "\r\n")}, nil
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
