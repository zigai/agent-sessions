package herdrctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/muxctx"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

var errPaneRequired = errors.New("herdr pane is required")

type Env struct {
	Enabled     string
	SessionName string
	SocketPath  string
	WorkspaceID string
	TabID       string
	PaneID      string
}

type CommandRunner func(context.Context, map[string]string, ...string) (string, error)

type ListOptions struct {
	Run      CommandRunner
	LookPath func(string) (string, error)
}

type CaptureOptions struct {
	Run CommandRunner
}

func Current() registry.MultiplexerContext {
	return CurrentWithEnv(Env{
		Enabled: os.Getenv("HERDR_ENV"), SessionName: os.Getenv("HERDR_SESSION"), SocketPath: os.Getenv("HERDR_SOCKET_PATH"),
		WorkspaceID: os.Getenv("HERDR_WORKSPACE_ID"), TabID: os.Getenv("HERDR_TAB_ID"), PaneID: os.Getenv("HERDR_PANE_ID"),
	})
}

func CurrentWithEnv(env Env) registry.MultiplexerContext {
	if strings.TrimSpace(env.PaneID) == "" || (env.Enabled != "1" && strings.TrimSpace(env.SocketPath) == "") {
		var empty registry.MultiplexerContext
		return empty
	}
	return registry.MultiplexerContext{ //nolint:exhaustruct // current environment exposes only managed Herdr identity fields
		Kind: registry.MultiplexerHerdr, ServerID: env.SocketPath, SessionName: env.SessionName,
		WorkspaceID: env.WorkspaceID, TabID: env.TabID, PaneID: env.PaneID,
	}
}

func ListPanes(ctx context.Context) ([]muxctx.Pane, error) {
	return ListPanesWithOptions(ctx, ListOptions{Run: nil, LookPath: nil})
}

//nolint:gocognit,cyclop // native session, snapshot, and process responses are intentionally joined here
func ListPanesWithOptions(ctx context.Context, options ListOptions) ([]muxctx.Pane, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list herdr panes: %w", err)
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if options.Run == nil {
		if _, err := lookPath("herdr"); err != nil {
			//nolint:nilerr // an optional unavailable multiplexer contributes no panes
			return nil, nil
		}
		options.Run = runHerdr
	}
	sessionOutput, err := options.Run(ctx, nil, "session", "list", "--json")
	if err != nil {
		if herdrUnavailable(sessionOutput) {
			return nil, nil
		}
		return nil, fmt.Errorf("list herdr sessions: %w", err)
	}
	sessions, err := parseSessions(sessionOutput)
	if err != nil {
		return nil, err
	}
	panes := make([]muxctx.Pane, 0)
	var listErrors []error
	for _, session := range sessions {
		env := map[string]string{"HERDR_SESSION": session}
		output, snapshotErr := options.Run(ctx, env, "api", "snapshot")
		if snapshotErr != nil {
			listErrors = append(listErrors, fmt.Errorf("read herdr session %q snapshot: %w", session, snapshotErr))
			continue
		}
		sessionPanes, parseErr := parseSnapshot(session, output)
		if parseErr != nil {
			return nil, parseErr
		}
		for index := range sessionPanes {
			processOutput, processErr := options.Run(ctx, env, "pane", "process-info", "--pane", sessionPanes[index].Location.PaneID)
			if processErr != nil {
				listErrors = append(listErrors, fmt.Errorf("read herdr pane process info: %w", processErr))
				continue
			}
			refs, processGroupID, parseProcessErr := parseProcessInfo(processOutput)
			if parseProcessErr != nil {
				return nil, parseProcessErr
			}
			sessionPanes[index].Processes = refs
			if sessionPanes[index].Location.PanePID == 0 && len(refs) > 0 {
				sessionPanes[index].Location.PanePID = refs[0].PID
			}
			for refIndex := range sessionPanes[index].Processes {
				if sessionPanes[index].Processes[refIndex].ProcessGroupID == 0 {
					sessionPanes[index].Processes[refIndex].ProcessGroupID = processGroupID
				}
			}
		}
		panes = append(panes, sessionPanes...)
	}
	return panes, errors.Join(listErrors...)
}

func CapturePane(ctx context.Context, pane muxctx.Pane) (muxctx.ScreenSnapshot, error) {
	return CapturePaneWithOptions(ctx, pane, CaptureOptions{Run: nil})
}

func CapturePaneWithOptions(ctx context.Context, pane muxctx.Pane, options CaptureOptions) (muxctx.ScreenSnapshot, error) {
	if pane.Location.Kind != registry.MultiplexerHerdr || pane.Location.PaneID == "" {
		return muxctx.ScreenSnapshot{}, errPaneRequired
	}
	run := options.Run
	if run == nil {
		run = runHerdr
	}
	env := map[string]string{}
	if pane.Location.SessionName != "" {
		env["HERDR_SESSION"] = pane.Location.SessionName
	}
	if pane.Location.ServerID != "" {
		env["HERDR_SOCKET_PATH"] = pane.Location.ServerID
	}
	output, err := run(ctx, env, "pane", "read", pane.Location.PaneID, "--source", "detection")
	if err != nil {
		return muxctx.ScreenSnapshot{}, fmt.Errorf("capture herdr pane: %w", err)
	}
	return muxctx.ScreenSnapshot{Text: parseReadOutput(output), Title: pane.Title}, nil
}

func parseSessions(output string) ([]string, error) {
	root, err := decodeJSON(output)
	if err != nil {
		return nil, fmt.Errorf("parse herdr sessions: %w", err)
	}
	records := findArray(root, "sessions")
	if records == nil {
		if direct, ok := root.([]any); ok {
			records = direct
		}
	}
	var sessions []string
	for _, item := range records {
		if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
			sessions = append(sessions, name)
			continue
		}
		record, ok := item.(map[string]any)
		if !ok || boolField(record, "exited", "dead") {
			continue
		}
		name := stringField(record, "name", "session_name", "id")
		if name != "" {
			sessions = append(sessions, name)
		}
	}
	return sessions, nil
}

func parseSnapshot(session string, output string) ([]muxctx.Pane, error) {
	root, err := decodeJSON(output)
	if err != nil {
		return nil, fmt.Errorf("parse herdr snapshot: %w", err)
	}
	agentsByPane := make(map[string]map[string]any)
	for _, item := range findArray(root, "agents") {
		if agent, ok := item.(map[string]any); ok {
			if paneID := stringField(agent, "pane_id"); paneID != "" {
				agentsByPane[paneID] = agent
			}
		}
	}
	workspaceNames := namesByID(findArray(root, "workspaces"), "workspace_id")
	tabNames := namesByID(findArray(root, "tabs"), "tab_id")
	records := findArray(root, "panes")
	panes := make([]muxctx.Pane, 0, len(records))
	for _, item := range records {
		record, ok := item.(map[string]any)
		if !ok || boolField(record, "exited", "closed") {
			continue
		}
		paneID := stringField(record, "pane_id", "id")
		if paneID == "" {
			continue
		}
		agent := agentsByPane[paneID]
		status := firstNonEmpty(stringField(record, "agent_status", "status"), stringField(agent, "agent_status", "status", "state"))
		activity := herdrActivity(status)
		workspaceID := stringField(record, "workspace_id")
		tabID := stringField(record, "tab_id")
		cwd := firstNonEmpty(stringField(record, "foreground_cwd"), stringField(record, "cwd"))
		label := firstNonEmpty(stringField(agent, "agent", "name", "label"), stringField(record, "agent", "agent_name"))
		location := registry.MultiplexerContext{ //nolint:exhaustruct // snapshot omits server, window, TTY, and process fields
			Kind: registry.MultiplexerHerdr, SessionName: session,
			WorkspaceID: workspaceID, WorkspaceName: workspaceNames[workspaceID],
			TabID: tabID, TabName: tabNames[tabID], PaneID: paneID, PaneCurrentPath: cwd,
		}
		panes = append(panes, muxctx.Pane{ //nolint:exhaustruct // process references are enriched by pane process-info below
			Location: location, CWD: cwd, Command: label,
			Title:    firstNonEmpty(stringField(record, "terminal_title_stripped", "title", "label"), label),
			Activity: activity, StateReason: "herdr_agent_status",
		})
	}
	return panes, nil
}

func parseProcessInfo(output string) ([]muxctx.ProcessRef, int, error) {
	root, err := decodeJSON(output)
	if err != nil {
		return nil, 0, fmt.Errorf("parse herdr process info: %w", err)
	}
	processGroupID := findInt(root, "foreground_process_group_id", "foreground_pgid")
	records := findArray(root, "foreground_processes")
	refs := make([]muxctx.ProcessRef, 0, len(records)+1)
	for _, item := range records {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pid := intField(record, "pid")
		if pid <= 0 {
			continue
		}
		command := stringField(record, "cmdline", "command", "name")
		if command == "" {
			if argv := stringSliceField(record, "argv", "args"); len(argv) > 0 {
				command = strings.Join(argv, " ")
			}
		}
		refs = append(refs, muxctx.ProcessRef{PID: pid, ProcessGroupID: processGroupID, Command: command, CWD: stringField(record, "cwd")})
	}
	if shellPID := findInt(root, "shell_pid"); shellPID > 0 {
		refs = append(refs, muxctx.ProcessRef{PID: shellPID, ProcessGroupID: 0, Command: "", CWD: ""})
	}
	return refs, processGroupID, nil
}

func parseReadOutput(output string) string {
	root, err := decodeJSON(output)
	if err != nil {
		return output
	}
	if text := findString(root, "text", "content", "output"); text != "" {
		return text
	}
	return output
}

func herdrActivity(status string) *registry.Activity {
	var activity registry.Activity
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "working", "running":
		activity = registry.ActivityRunning
	case "blocked", "waiting":
		activity = registry.ActivityWaiting
	case "idle", "done":
		activity = registry.ActivityIdle
	case "unknown", "":
		return nil
	default:
		return nil
	}
	return &activity
}

func decodeJSON(output string) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return nil, fmt.Errorf("decode herdr JSON: %w", err)
	}
	return value, nil
}

func findArray(value any, key string) []any {
	switch typed := value.(type) {
	case map[string]any:
		if array, ok := typed[key].([]any); ok {
			return array
		}
		for _, nested := range typed {
			if array := findArray(nested, key); array != nil {
				return array
			}
		}
	case []any:
		for _, nested := range typed {
			if array := findArray(nested, key); array != nil {
				return array
			}
		}
	}
	return nil
}

func findInt(value any, keys ...string) int {
	switch typed := value.(type) {
	case map[string]any:
		if result := intField(typed, keys...); result != 0 {
			return result
		}
		for _, nested := range typed {
			if result := findInt(nested, keys...); result != 0 {
				return result
			}
		}
	case []any:
		for _, nested := range typed {
			if result := findInt(nested, keys...); result != 0 {
				return result
			}
		}
	}
	return 0
}

func findString(value any, keys ...string) string {
	switch typed := value.(type) {
	case map[string]any:
		if result := stringField(typed, keys...); result != "" {
			return result
		}
		for _, nested := range typed {
			if result := findString(nested, keys...); result != "" {
				return result
			}
		}
	case []any:
		for _, nested := range typed {
			if result := findString(nested, keys...); result != "" {
				return result
			}
		}
	}
	return ""
}

func stringField(record map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := record[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return value
			}
		case json.Number:
			return value.String()
		case float64:
			return strconv.FormatInt(int64(value), 10)
		}
	}
	return ""
}

func intField(record map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := record[key].(type) {
		case float64:
			return int(value)
		case json.Number:
			parsed, _ := strconv.Atoi(value.String())
			return parsed
		case string:
			parsed, _ := strconv.Atoi(value)
			return parsed
		}
	}
	return 0
}

func boolField(record map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := record[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func stringSliceField(record map[string]any, keys ...string) []string {
	for _, key := range keys {
		values, ok := record[key].([]any)
		if !ok {
			continue
		}
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

func namesByID(records []any, idKey string) map[string]string {
	result := make(map[string]string)
	for _, item := range records {
		if record, ok := item.(map[string]any); ok {
			id := stringField(record, idKey, "id")
			if id != "" {
				result[id] = stringField(record, "name", "label", "title")
			}
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func herdrUnavailable(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "not running") || strings.Contains(value, "no session") || strings.Contains(value, "connection refused")
}

func runHerdr(ctx context.Context, env map[string]string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "herdr", args...)
	command.Env = make([]string, 0, len(os.Environ())+len(env))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := env[key]; !overridden {
			command.Env = append(command.Env, entry)
		}
	}
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return string(output), fmt.Errorf("run herdr command: %w", ctxErr)
		}
		return string(output), fmt.Errorf("run herdr command: %w", err)
	}
	return string(output), nil
}
