// Package muxctx provides backward-compatible aliases for [github.com/zigai/aht/pkg/mux].
//
// Deprecated: Use [github.com/zigai/aht/pkg/mux] instead.
package muxctx

import (
	"github.com/zigai/aht/pkg/mux"
)

type (
	ProcessRef     = mux.ProcessRef
	Pane           = mux.Pane
	ScreenSnapshot = mux.ScreenSnapshot
	PaneLister     = mux.PaneLister
	ScreenCapturer = mux.ScreenCapturer
)

func BoundBottomLines(text string, limit int) []string {
	return mux.BoundBottomLines(text, limit)
}
