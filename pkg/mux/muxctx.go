package mux

import (
	"context"
	"strings"

	"github.com/zigai/aht/pkg/registry"
)

// ProcessRef identifies a process reported by a multiplexer for one pane.
// Start identity is resolved from the observer's own process snapshot.
type ProcessRef struct {
	PID            int
	ProcessGroupID int
	Command        string
	CWD            string
}

// Pane is transient native multiplexer inventory. Only Location is persisted.
type Pane struct {
	Location    registry.MultiplexerContext
	Processes   []ProcessRef
	ProcessTTY  string
	Command     string
	CWD         string
	Title       string
	Activity    *registry.Activity
	StateReason string
}

type ScreenSnapshot struct {
	Text  string
	Title string
}

type (
	PaneLister     func(context.Context) ([]Pane, error)
	ScreenCapturer func(context.Context, Pane) (ScreenSnapshot, error)
)

// BoundBottomLines normalizes line endings, strips any trailing empty line, and returns at most limit bottom lines.
func BoundBottomLines(text string, limit int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}
