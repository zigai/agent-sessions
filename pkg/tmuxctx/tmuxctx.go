// Package tmuxctx provides backward-compatible aliases for [github.com/zigai/aht/pkg/tmux].
//
// Deprecated: Use [github.com/zigai/aht/pkg/tmux] instead.
package tmuxctx

import (
	"github.com/zigai/aht/pkg/tmux"
)

type (
	Pane                = tmux.Pane
	Env                 = tmux.Env
	CommandRunner       = tmux.CommandRunner
	ServerProcess       = tmux.ServerProcess
	ServerProcessLister = tmux.ServerProcessLister
	ListOptions         = tmux.ListOptions
)

var (
	ErrNoTmuxContext     = tmux.ErrNoTmuxContext
	ErrInvalidFieldCount = tmux.ErrInvalidFieldCount

	Current              = tmux.Current
	CurrentWithEnv       = tmux.CurrentWithEnv
	ContextFromEnv       = tmux.ContextFromEnv
	ListPanes            = tmux.ListPanes
	ListPanesWithOptions = tmux.ListPanesWithOptions
	SendInterruptTo      = tmux.SendInterruptTo
	ParseCurrent         = tmux.ParseCurrent
	ParseListPanes       = tmux.ParseListPanes
)
