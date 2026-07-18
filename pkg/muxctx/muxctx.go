package muxctx

import (
	"context"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
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
