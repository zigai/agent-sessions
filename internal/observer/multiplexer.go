package observer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zigai/agent-sessions/v2/internal/processinfo"
	"github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/herdrctx"
	"github.com/zigai/agent-sessions/v2/pkg/muxctx"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
	"github.com/zigai/agent-sessions/v2/pkg/zellijctx"
)

var errUnsupportedMultiplexerPane = errors.New("unsupported multiplexer pane")

const (
	multiplexerPriorityTmux   = 1
	multiplexerPriorityZellij = 2
	multiplexerPriorityHerdr  = 3
)

func listMultiplexerPanes(ctx context.Context) ([]muxctx.Pane, error) {
	panes := make([]muxctx.Pane, 0)
	var listErrors []error
	if _, err := exec.LookPath("tmux"); err == nil {
		tmuxPanes, listErr := tmuxctx.ListPanes(ctx)
		if listErr != nil {
			listErrors = append(listErrors, listErr)
		} else {
			panes = append(panes, multiplexerPanesFromTmux(tmuxPanes)...)
		}
	}
	for _, list := range []muxctx.PaneLister{zellijctx.ListPanes, herdrctx.ListPanes} {
		listed, listErr := list(ctx)
		if listErr != nil {
			listErrors = append(listErrors, listErr)
		}
		panes = append(panes, listed...)
	}
	return panes, errors.Join(listErrors...)
}

func multiplexerPanesFromTmux(panes []tmuxctx.Pane) []muxctx.Pane {
	result := make([]muxctx.Pane, 0, len(panes))
	for _, pane := range panes {
		location := registry.MultiplexerFromTmux(pane.Tmux)
		if location.ServerID == "" {
			location.ServerID = pane.ServerIdentity
		}
		location.PanePID = pane.PanePID
		location.PaneTTY = pane.PaneTTY
		processes := make([]muxctx.ProcessRef, 0, 1)
		if pane.PanePID > 0 {
			processes = append(processes, muxctx.ProcessRef{PID: pane.PanePID, ProcessGroupID: 0, Command: "", CWD: ""})
		}
		result = append(result, muxctx.Pane{
			Location: location, Processes: processes, ProcessTTY: pane.PaneTTY,
			Command: "", CWD: location.PaneCurrentPath, Title: "", Activity: nil, StateReason: "",
		})
	}
	return result
}

func legacyTmuxPaneLister(list PaneLister) muxctx.PaneLister {
	return func(ctx context.Context) ([]muxctx.Pane, error) {
		panes, err := list(ctx)
		return multiplexerPanesFromTmux(panes), err
	}
}

func captureMultiplexerPane(ctx context.Context, pane muxctx.Pane) (muxctx.ScreenSnapshot, error) {
	switch pane.Location.Kind {
	case registry.MultiplexerTmux:
		tmuxPane := tmuxctx.Pane{
			Tmux: pane.Location.TmuxContext(), ServerIdentity: pane.Location.ServerID,
			PanePID: pane.Location.PanePID, PaneTTY: pane.Location.PaneTTY,
		}
		snapshot, err := tmuxctx.CapturePane(ctx, tmuxPane)
		if err != nil {
			return muxctx.ScreenSnapshot{}, fmt.Errorf("capture tmux pane: %w", err)
		}
		return muxctx.ScreenSnapshot{Text: snapshot.Text, Title: snapshot.Title}, nil
	case registry.MultiplexerZellij:
		snapshot, err := zellijctx.CapturePane(ctx, pane)
		if err != nil {
			return muxctx.ScreenSnapshot{}, fmt.Errorf("capture zellij pane: %w", err)
		}
		return snapshot, nil
	case registry.MultiplexerHerdr:
		snapshot, err := herdrctx.CapturePane(ctx, pane)
		if err != nil {
			return muxctx.ScreenSnapshot{}, fmt.Errorf("capture herdr pane: %w", err)
		}
		return snapshot, nil
	default:
		return muxctx.ScreenSnapshot{}, errUnsupportedMultiplexerPane
	}
}

func legacyTmuxScreenCapturer(capture ScreenCapturer) muxctx.ScreenCapturer {
	return func(ctx context.Context, pane muxctx.Pane) (muxctx.ScreenSnapshot, error) {
		tmuxPane := tmuxctx.Pane{
			Tmux: pane.Location.TmuxContext(), ServerIdentity: pane.Location.ServerID,
			PanePID: pane.Location.PanePID, PaneTTY: pane.Location.PaneTTY,
		}
		snapshot, err := capture(ctx, tmuxPane)
		return muxctx.ScreenSnapshot{Text: snapshot.Text, Title: snapshot.Title}, err
	}
}

func multiplexerPaneProcess(pane muxctx.Pane, processes []processinfo.Process, byPID map[int]processinfo.Process, harnessByPID map[int]registry.Harness, paneCommandCounts map[string]int) (processinfo.Process, registry.Harness, bool) {
	if process, harnessID, ok := foregroundPaneProcess(pane, processes, harnessByPID); ok {
		return process, harnessID, true
	}
	for _, reference := range pane.Processes {
		if process, ok := byPID[reference.PID]; ok {
			if harnessID, ok := harnessByPID[process.PID]; ok {
				return process, harnessID, true
			}
		}
		if process, harnessID, ok := descendantHarnessProcess(reference.PID, processes, byPID, harnessByPID); ok {
			return process, harnessID, true
		}
	}
	if process, harnessID, ok := processMatchingMultiplexerIdentity(pane, processes, harnessByPID); ok {
		return process, harnessID, true
	}
	if process, harnessID, ok := commandPaneProcess(pane, processes, harnessByPID, paneCommandCounts); ok {
		return process, harnessID, true
	}
	var empty processinfo.Process
	return empty, "", false
}

func processMatchingMultiplexerIdentity(pane muxctx.Pane, processes []processinfo.Process, harnessByPID map[int]registry.Harness) (processinfo.Process, registry.Harness, bool) {
	var selected processinfo.Process
	var selectedHarness registry.Harness
	for _, process := range processes {
		harnessID, ok := harnessByPID[process.PID]
		if !ok || !multiplexerIdentityMatches(process, pane.Location) {
			continue
		}
		if selected.PID == 0 || preferForegroundProcess(process, selected) {
			selected, selectedHarness = process, harnessID
		}
	}
	return selected, selectedHarness, selected.PID != 0
}

//nolint:cyclop // each multiplexer has different native identity guarantees
func multiplexerIdentityMatches(process processinfo.Process, location registry.MultiplexerContext) bool {
	if process.MultiplexerKind == "" || process.MultiplexerKind != string(location.Kind) {
		return false
	}
	switch location.Kind {
	case registry.MultiplexerZellij:
		if process.MultiplexerSession == "" || location.SessionName == "" || process.MultiplexerSession != location.SessionName {
			return false
		}
	case registry.MultiplexerHerdr:
		sameServer := process.MultiplexerServer != "" && location.ServerID != "" && process.MultiplexerServer == location.ServerID
		sameSession := process.MultiplexerSession != "" && location.SessionName != "" && process.MultiplexerSession == location.SessionName
		if !sameServer && !sameSession {
			return false
		}
	case registry.MultiplexerTmux:
		return false
	}
	return normalizeMultiplexerPaneID(location.Kind, process.MultiplexerPane) == normalizeMultiplexerPaneID(location.Kind, location.PaneID)
}

func normalizeMultiplexerPaneID(kind registry.MultiplexerKind, paneID string) string {
	paneID = strings.TrimSpace(paneID)
	if kind == registry.MultiplexerZellij && paneID != "" && !strings.HasPrefix(paneID, "terminal_") && !strings.HasPrefix(paneID, "plugin_") {
		return "terminal_" + paneID
	}
	return paneID
}

func foregroundPaneProcess(pane muxctx.Pane, processes []processinfo.Process, harnessByPID map[int]registry.Harness) (processinfo.Process, registry.Harness, bool) {
	var selected processinfo.Process
	var selectedHarness registry.Harness
	for _, process := range processes {
		if !process.Foreground || pane.ProcessTTY == "" || process.TTY != pane.ProcessTTY {
			continue
		}
		harnessID, ok := harnessByPID[process.PID]
		if !ok {
			continue
		}
		if selected.PID == 0 || preferForegroundProcess(process, selected) {
			selected, selectedHarness = process, harnessID
		}
	}
	return selected, selectedHarness, selected.PID != 0
}

func descendantHarnessProcess(rootPID int, processes []processinfo.Process, byPID map[int]processinfo.Process, harnessByPID map[int]registry.Harness) (processinfo.Process, registry.Harness, bool) {
	if rootPID <= 0 {
		var empty processinfo.Process
		return empty, "", false
	}
	for _, process := range processes {
		ancestor := process
		for range processes {
			if ancestor.PPID == rootPID {
				if harnessID, ok := harnessByPID[process.PID]; ok {
					return process, harnessID, true
				}
			}
			next, ok := byPID[ancestor.PPID]
			if !ok || next.PID == ancestor.PID {
				break
			}
			ancestor = next
		}
	}
	var empty processinfo.Process
	return empty, "", false
}

func commandPaneProcess(pane muxctx.Pane, processes []processinfo.Process, harnessByPID map[int]registry.Harness, paneCommandCounts map[string]int) (processinfo.Process, registry.Harness, bool) {
	key, paneHarness, ok := commandPaneKey(pane)
	if !ok || paneCommandCounts[key] != 1 {
		var empty processinfo.Process
		return empty, "", false
	}
	var selected processinfo.Process
	for _, process := range processes {
		if harnessByPID[process.PID] != paneHarness {
			continue
		}
		if process.CWD == "" || pane.CWD != process.CWD {
			continue
		}
		if selected.PID != 0 {
			var empty processinfo.Process
			return empty, "", false
		}
		selected = process
	}
	return selected, paneHarness, selected.PID != 0
}

func commandPaneCounts(panes []muxctx.Pane) map[string]int {
	counts := make(map[string]int)
	for _, pane := range panes {
		if key, _, ok := commandPaneKey(pane); ok {
			counts[key]++
		}
	}
	return counts
}

func commandPaneKey(pane muxctx.Pane) (string, registry.Harness, bool) {
	if pane.CWD == "" {
		return "", "", false
	}
	fields := strings.Fields(pane.Command)
	if len(fields) == 0 {
		return "", "", false
	}
	paneHarness, ok := harness.FromCommand(filepath.Base(fields[0]))
	if !ok {
		return "", "", false
	}
	return string(paneHarness) + "\x00" + filepath.Clean(pane.CWD), paneHarness, true
}

func multiplexerPriority(kind registry.MultiplexerKind) int {
	switch kind {
	case registry.MultiplexerHerdr:
		return multiplexerPriorityHerdr
	case registry.MultiplexerZellij:
		return multiplexerPriorityZellij
	case registry.MultiplexerTmux:
		return multiplexerPriorityTmux
	default:
		return 0
	}
}
