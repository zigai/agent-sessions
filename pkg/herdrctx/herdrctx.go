// Package herdrctx provides backward-compatible aliases for [github.com/zigai/aht/pkg/herdr].
//
// Deprecated: Use [github.com/zigai/aht/pkg/herdr] instead.
package herdrctx

import (
	"github.com/zigai/aht/pkg/herdr"
)

type (
	Env           = herdr.Env
	CommandRunner = herdr.CommandRunner
	ListOptions   = herdr.ListOptions
)

var (
	Current              = herdr.Current
	CurrentWithEnv       = herdr.CurrentWithEnv
	ListPanes            = herdr.ListPanes
	ListPanesWithOptions = herdr.ListPanesWithOptions
)
