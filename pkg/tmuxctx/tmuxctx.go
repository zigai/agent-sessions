package tmuxctx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	fieldSeparator            = "\t"
	escapedFieldPrefix        = "tmuxctx:"
	listPaneFieldCount        = 11
	singleQuoteDelimiterWidth = 2
)

var (
	// ErrNoTmuxContext is returned when no tmux environment is available.
	ErrNoTmuxContext = errors.New("not inside tmux")
	// ErrInvalidFieldCount is returned when tmux output does not match the requested format.
	ErrInvalidFieldCount             = errors.New("invalid tmux field count")
	errMissingTmuxPaneID             = errors.New("missing tmux pane id")
	errUnterminatedSingleQuotedField = errors.New("unterminated tmux single-quoted field")
	errUnterminatedDoubleQuotedField = errors.New("unterminated tmux double-quoted field")
)

type Pane struct {
	Tmux           registry.TmuxContext
	CurrentCommand string
}

func Current(ctx context.Context) (registry.TmuxContext, error) {
	if os.Getenv("TMUX") == "" && os.Getenv("TMUX_PANE") == "" {
		return registry.TmuxContext{}, ErrNoTmuxContext
	}

	format := currentFormat()
	output, err := runTmux(ctx, currentDisplayMessageArgs(format, os.Getenv("TMUX_PANE"))...)
	if err != nil {
		if paneID := os.Getenv("TMUX_PANE"); paneID != "" {
			return registry.TmuxContext{
				Inside:          true,
				ServerSocket:    tmuxServerSocketFromEnv(),
				SessionID:       "",
				SessionName:     "",
				WindowID:        "",
				WindowIndex:     "",
				WindowName:      "",
				PaneID:          paneID,
				PaneIndex:       "",
				PaneCurrentPath: "",
				PanePID:         0,
				PaneTTY:         "",
				ClientTTY:       "",
			}, nil
		}

		return registry.TmuxContext{}, err
	}

	current, err := ParseCurrent(output)
	if err != nil {
		return registry.TmuxContext{}, err
	}
	current.ServerSocket = tmuxServerSocketFromEnv()

	return current, nil
}

func currentDisplayMessageArgs(format string, paneID string) []string {
	args := []string{"display-message", "-p"}
	if paneID != "" {
		args = append(args, "-t", paneID)
	}

	return append(args, "-F", format)
}

func ListPanes(ctx context.Context) ([]Pane, error) {
	format := listPanesFormat()
	output, err := runTmux(ctx, "list-panes", "-a", "-F", format)
	if err != nil {
		return nil, err
	}

	panes, err := ParseListPanes(output)
	if err != nil {
		return nil, err
	}
	serverSocket := tmuxServerSocketFromEnv()
	for index := range panes {
		panes[index].Tmux.ServerSocket = serverSocket
	}

	return panes, nil
}

func currentFormat() string {
	return tmuxFormat([]string{
		"session_id",
		"session_name",
		"window_id",
		"window_index",
		"window_name",
		"pane_id",
		"pane_index",
		"pane_current_path",
		"pane_pid",
		"pane_tty",
		"client_tty",
	})
}

func listPanesFormat() string {
	return tmuxFormat([]string{
		"session_id",
		"session_name",
		"window_id",
		"window_index",
		"window_name",
		"pane_id",
		"pane_index",
		"pane_current_path",
		"pane_pid",
		"pane_tty",
		"pane_current_command",
	})
}

func SendInterrupt(ctx context.Context, paneID string) error {
	if strings.TrimSpace(paneID) == "" {
		return errMissingTmuxPaneID
	}
	_, err := runTmux(ctx, "send-keys", "-t", paneID, "C-c")
	if err != nil {
		return err
	}

	return nil
}

func ParseCurrent(output string) (registry.TmuxContext, error) {
	const expectedFields = listPaneFieldCount
	fields, err := parseTmuxFields(output, expectedFields)
	if err != nil {
		return registry.TmuxContext{}, err
	}
	if len(fields) != expectedFields {
		return registry.TmuxContext{}, invalidFieldCountError(expectedFields, len(fields))
	}

	return registry.TmuxContext{
		Inside:          true,
		ServerSocket:    "",
		SessionID:       fields[0],
		SessionName:     fields[1],
		WindowID:        fields[2],
		WindowIndex:     fields[3],
		WindowName:      fields[4],
		PaneID:          fields[5],
		PaneIndex:       fields[6],
		PaneCurrentPath: fields[7],
		PanePID:         parsePositiveInt(fields[8]),
		PaneTTY:         fields[9],
		ClientTTY:       fields[10],
	}, nil
}

func ParseListPanes(output string) ([]Pane, error) {
	trimmed := strings.TrimRight(output, "\r\n")
	if trimmed == "" {
		return nil, nil
	}

	fields, ok, err := parseEscapedFields(trimmed)
	if err != nil {
		return nil, err
	}
	if ok {
		return panesFromFields(fields)
	}

	return parseLegacyListPanes(trimmed)
}

func panesFromFields(fields []string) ([]Pane, error) {
	const expectedFields = listPaneFieldCount
	if len(fields)%expectedFields != 0 {
		return nil, invalidFieldCountError(expectedFields, len(fields))
	}

	panes := make([]Pane, 0, len(fields)/expectedFields)
	for offset := 0; offset < len(fields); offset += expectedFields {
		paneFields := fields[offset : offset+expectedFields]

		panes = append(panes, Pane{
			Tmux: registry.TmuxContext{
				Inside:          true,
				ServerSocket:    "",
				SessionID:       paneFields[0],
				SessionName:     paneFields[1],
				WindowID:        paneFields[2],
				WindowIndex:     paneFields[3],
				WindowName:      paneFields[4],
				PaneID:          paneFields[5],
				PaneIndex:       paneFields[6],
				PaneCurrentPath: paneFields[7],
				PanePID:         parsePositiveInt(paneFields[8]),
				PaneTTY:         paneFields[9],
				ClientTTY:       "",
			},
			CurrentCommand: paneFields[10],
		})
	}

	return panes, nil
}

func parseLegacyListPanes(output string) ([]Pane, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}

	fields := make([]string, 0, len(lines)*listPaneFieldCount)
	const expectedFields = listPaneFieldCount
	for _, line := range lines {
		lineFields := splitLegacyFields(line, expectedFields)
		if len(lineFields) != expectedFields {
			return nil, invalidFieldCountError(expectedFields, len(lineFields))
		}
		fields = append(fields, lineFields...)
	}

	return panesFromFields(fields)
}

func tmuxFormat(fields []string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, escapedFieldPrefix+"#{q:"+field+"}")
	}

	return strings.Join(parts, " ")
}

func tmuxServerSocketFromEnv() string {
	tmuxEnv := strings.TrimSpace(os.Getenv("TMUX"))
	if tmuxEnv == "" {
		return ""
	}

	socket, _, _ := strings.Cut(tmuxEnv, ",")

	return socket
}

func parseTmuxFields(output string, expectedFields int) ([]string, error) {
	trimmed := strings.TrimRight(output, "\r\n")
	if trimmed == "" {
		return nil, nil
	}

	escapedFields, ok, err := parseEscapedFields(trimmed)
	if err != nil {
		return nil, err
	}
	if ok {
		return escapedFields, nil
	}

	return splitLegacyFields(trimmed, expectedFields), nil
}

func parseEscapedFields(output string) ([]string, bool, error) {
	if !strings.Contains(output, escapedFieldPrefix) {
		return nil, false, nil
	}

	words, err := shellWords(output)
	if err != nil {
		return nil, false, err
	}
	if len(words) == 0 {
		return nil, false, nil
	}

	fields := make([]string, 0, len(words))
	for _, word := range words {
		if !strings.HasPrefix(word, escapedFieldPrefix) {
			return nil, false, nil
		}
		fields = append(fields, strings.TrimPrefix(word, escapedFieldPrefix))
	}

	return fields, true, nil
}

func shellWords(input string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	for index := 0; index < len(input); {
		char := input[index]
		switch {
		case isShellWhitespace(char):
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
			index++
		case char == '\'':
			inWord = true
			next := strings.IndexByte(input[index+1:], '\'')
			if next < 0 {
				return nil, errUnterminatedSingleQuotedField
			}
			current.WriteString(input[index+1 : index+1+next])
			index += next + singleQuoteDelimiterWidth
		case char == '"':
			inWord = true
			nextIndex, err := appendDoubleQuotedShellWord(&current, input, index+1)
			if err != nil {
				return nil, err
			}
			index = nextIndex
		case char == '\\':
			inWord = true
			if index+1 >= len(input) {
				current.WriteByte(char)
				index++
				continue
			}
			current.WriteByte(input[index+1])
			index += 2
		default:
			inWord = true
			current.WriteByte(char)
			index++
		}
	}
	if inWord {
		words = append(words, current.String())
	}

	return words, nil
}

func appendDoubleQuotedShellWord(current *strings.Builder, input string, index int) (int, error) {
	for index < len(input) {
		char := input[index]
		switch char {
		case '"':
			return index + 1, nil
		case '\\':
			if index+1 >= len(input) {
				current.WriteByte(char)
				index++
				continue
			}
			current.WriteByte(input[index+1])
			index += 2
		default:
			current.WriteByte(char)
			index++
		}
	}

	return index, errUnterminatedDoubleQuotedField
}

func isShellWhitespace(char byte) bool {
	return char == ' ' || char == '\n' || char == '\r'
}

func splitLegacyFields(output string, expectedFields int) []string {
	fields := strings.Split(output, fieldSeparator)
	if len(fields) > expectedFields && expectedFields > 8 {
		pathParts := len(fields) - expectedFields + 1
		merged := make([]string, 0, expectedFields)
		merged = append(merged, fields[:7]...)
		merged = append(merged, strings.Join(fields[7:7+pathParts], fieldSeparator))
		merged = append(merged, fields[7+pathParts:]...)

		return merged
	}

	return fields
}

func invalidFieldCountError(expected int, got int) error {
	return fmt.Errorf("%w: expected %d, got %d", ErrInvalidFieldCount, expected, got)
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}

	return parsed
}

func runTmux(ctx context.Context, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("running tmux %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}
