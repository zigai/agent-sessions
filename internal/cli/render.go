package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	prettytable "github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const (
	humanLineWidth      = 120
	humanColumnGap      = 2
	humanDetailLabelMax = 24
)

var (
	errHumanTableCellCount = errors.New("human table row has the wrong cell count")
	errHumanTableWidth     = errors.New("human table exceeds output width")
)

type humanWrapFunc func(string, int) []string

type humanColumn struct {
	heading string
	width   int
	wrap    humanWrapFunc
}

type humanDetail struct {
	label string
	value string
	wrap  humanWrapFunc
}

func (app *application) writeHumanTable(columns []humanColumn, rows [][]string) error {
	return app.writeHumanTableRows(columns, rows, false)
}

func (app *application) writeWrappedHumanTable(columns []humanColumn, rows [][]string) error {
	return app.writeHumanTableRows(columns, rows, true)
}

func (app *application) writeHumanTableRows(columns []humanColumn, rows [][]string, wrap bool) error {
	if err := validateHumanColumns(columns, app.maxLineWidth()); err != nil {
		return err
	}
	writer := prettytable.NewWriter()
	style := prettytable.StyleDefault
	style.Box.PaddingLeft = ""
	style.Box.PaddingRight = "  "
	style.Format.Header = text.FormatDefault
	style.Options = prettytable.OptionsNoBordersAndSeparators
	writer.SetStyle(style)

	header := make(prettytable.Row, len(columns))
	colConfigs := make([]prettytable.ColumnConfig, len(columns))
	for i, col := range columns {
		header[i] = col.heading
		colConfigs[i] = prettytable.ColumnConfig{
			Number:   i + 1,
			WidthMax: col.width,
		}
		if wrap {
			wrapCell := col.wrap
			if wrapCell == nil {
				wrapCell = wrapHumanText
			}
			colConfigs[i].WidthMaxEnforcer = func(value string, maxLen int) string {
				return strings.Join(wrapCell(value, maxLen), "\n")
			}
		} else {
			colConfigs[i].WidthMaxEnforcer = truncateHumanText
		}
	}
	writer.SetColumnConfigs(colConfigs)
	writer.AppendHeader(header)

	for _, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("%w: got %d cells for %d columns", errHumanTableCellCount, len(row), len(columns))
		}
		tableRow := make(prettytable.Row, len(row))
		for i, cell := range row {
			tableRow[i] = sanitizeHumanText(cell)
		}
		writer.AppendRow(tableRow)
	}

	writer.SuppressTrailingSpaces()
	rendered := writer.Render()
	if rendered == "" {
		return nil
	}
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	if _, err := fmt.Fprintln(app.stdout, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("write human table: %w", err)
	}
	return nil
}

func (app *application) writeStackedHumanRows(columns []humanColumn, rows [][]string) error {
	for rowIndex, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("%w: got %d cells for %d columns", errHumanTableCellCount, len(row), len(columns))
		}
		details := make([]humanDetail, len(columns))
		for columnIndex, column := range columns {
			details[columnIndex] = humanDetail{
				label: column.heading,
				value: row[columnIndex],
				wrap:  column.wrap,
			}
		}
		if err := app.writeHumanDetails(details); err != nil {
			return err
		}
		if rowIndex+1 < len(rows) {
			if err := app.writeln(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (app *application) maxLineWidth() int {
	if app != nil && app.stdout != nil {
		if w := terminalWidth(app.stdout); w > 0 {
			return w
		}
	}
	return humanLineWidth
}

func validateHumanColumns(columns []humanColumn, maxLineWidth int) error {
	if maxLineWidth <= 0 {
		maxLineWidth = humanLineWidth
	}
	width := max(0, len(columns)-1) * humanColumnGap
	for _, column := range columns {
		if column.width <= 0 {
			return fmt.Errorf("%w: invalid column width %d", errHumanTableWidth, column.width)
		}
		width += column.width
	}
	if width > maxLineWidth {
		return fmt.Errorf("%w: %d > %d", errHumanTableWidth, width, maxLineWidth)
	}
	return nil
}

func (app *application) writeHumanDetails(details []humanDetail) error {
	labelWidth := 0
	for _, detail := range details {
		labelWidth = max(labelWidth, utf8.RuneCountInString(detail.label)+1)
	}
	maxWidth := app.maxLineWidth()
	labelWidth = min(labelWidth, humanDetailLabelMax, max(1, maxWidth-humanColumnGap-1))
	valueWidth := max(1, maxWidth-labelWidth-humanColumnGap)
	for _, detail := range details {
		label := truncateHumanText(detail.label+":", labelWidth)
		wrapValue := detail.wrap
		if wrapValue == nil {
			wrapValue = wrapHumanText
		}
		lines := wrapValue(detail.value, valueWidth)
		for index, line := range lines {
			if index == 0 {
				if _, err := fmt.Fprintf(app.stdout, "%-*s  %s\n", labelWidth, label, line); err != nil {
					return fmt.Errorf("write human details: %w", err)
				}
				continue
			}
			if _, err := fmt.Fprintf(app.stdout, "%*s  %s\n", labelWidth, "", line); err != nil {
				return fmt.Errorf("write human details: %w", err)
			}
		}
	}
	return nil
}

func sanitizeHumanText(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || isBidiControl(character) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func isBidiControl(character rune) bool {
	switch character {
	case '\u061c', '\u200e', '\u200f':
		return true
	default:
		return character >= '\u202a' && character <= '\u202e' ||
			character >= '\u2066' && character <= '\u2069'
	}
}

func truncateHumanText(value string, width int) string {
	value = sanitizeHumanText(value)
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return strings.TrimRight(text.Snip(value, width, "…"), " ")
}

func wrapHumanText(value string, width int) []string {
	value = sanitizeHumanText(value)
	if value == "" {
		return []string{"-"}
	}
	wrapped := text.WrapSoft(value, width)
	lines := strings.Split(wrapped, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"-"}
	}
	return result
}

func wrapHumanPath(value string, width int) []string {
	return wrapDelimitedHumanText(value, width, `/\`)
}

func wrapHumanSession(value string, width int) []string {
	if strings.Contains(value, " ") {
		return wrapHumanText(value, width)
	}
	return wrapHumanIdentifier(value, width)
}

func wrapHumanIdentifier(value string, width int) []string {
	return wrapDelimitedHumanText(value, width, "-_./")
}

func wrapDelimitedHumanText(value string, width int, delimiters string) []string {
	value = sanitizeHumanText(value)
	if value == "" {
		return []string{"-"}
	}
	var lines []string
	for utf8.RuneCountInString(value) > width {
		runes := []rune(value)
		cut := width
		for index := width; index > 0; index-- {
			switch {
			case runes[index-1] == ' ':
				cut = index - 1
			case strings.ContainsRune(delimiters, runes[index-1]):
				cut = index
			default:
				continue
			}
			break
		}
		if cut == 0 {
			cut = width
		}
		lines = append(lines, strings.TrimSpace(string(runes[:cut])))
		value = strings.TrimSpace(string(runes[cut:]))
	}
	if value != "" {
		lines = append(lines, value)
	}
	if len(lines) == 0 {
		return []string{"-"}
	}
	return lines
}
