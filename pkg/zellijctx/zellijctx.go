// Package zellijctx provides backward-compatible aliases for [github.com/zigai/aht/pkg/zellij].
//
// Deprecated: Use [github.com/zigai/aht/pkg/zellij] instead.
package zellijctx

import (
	"github.com/zigai/aht/pkg/zellij"
)

type (
	Env            = zellij.Env
	CommandRunner  = zellij.CommandRunner
	ListOptions    = zellij.ListOptions
	CaptureOptions = zellij.CaptureOptions
)

var (
	Current                = zellij.Current
	CurrentWithEnv         = zellij.CurrentWithEnv
	ListPanes              = zellij.ListPanes
	ListPanesWithOptions   = zellij.ListPanesWithOptions
	CapturePane            = zellij.CapturePane
	CapturePaneWithOptions = zellij.CapturePaneWithOptions
)
