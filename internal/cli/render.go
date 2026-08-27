package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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

type humanColumn struct {
	heading string
	width   int
}

type humanDetail struct {
	label string
	value string
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
	headings := make([]string, len(columns))
	for index, column := range columns {
		headings[index] = column.heading
	}
	if err := app.writeHumanTableLine(columns, headings); err != nil {
		return err
	}
	for _, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("%w: got %d cells for %d columns", errHumanTableCellCount, len(row), len(columns))
		}
		if !wrap {
			if err := app.writeHumanTableLine(columns, row); err != nil {
				return err
			}
			continue
		}
		if err := app.writeWrappedHumanTableRow(columns, row); err != nil {
			return err
		}
	}
	return nil
}

func (app *application) writeWrappedHumanTableRow(columns []humanColumn, row []string) error {
	wrapped := make([][]string, len(columns))
	lineCount := 1
	for index, cell := range row {
		wrapped[index] = wrapHumanText(cell, columns[index].width)
		lineCount = max(lineCount, len(wrapped[index]))
	}
	for lineIndex := range lineCount {
		line := make([]string, len(columns))
		for columnIndex := range columns {
			if lineIndex < len(wrapped[columnIndex]) {
				line[columnIndex] = wrapped[columnIndex][lineIndex]
			}
		}
		if err := app.writeHumanTableLine(columns, line); err != nil {
			return err
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

func (app *application) writeHumanTableLine(columns []humanColumn, cells []string) error {
	var line strings.Builder
	for index, column := range columns {
		if index > 0 {
			line.WriteString(strings.Repeat(" ", humanColumnGap))
		}
		cell := truncateHumanText(sanitizeHumanText(cells[index]), column.width)
		line.WriteString(cell)
		if index+1 < len(columns) {
			line.WriteString(strings.Repeat(" ", column.width-utf8.RuneCountInString(cell)))
		}
	}
	if _, err := fmt.Fprintln(app.stdout, strings.TrimRight(line.String(), " ")); err != nil {
		return fmt.Errorf("write human table: %w", err)
	}
	return nil
}

func (app *application) writeHumanDetails(details []humanDetail) error {
	labelWidth := 0
	for _, detail := range details {
		labelWidth = max(labelWidth, utf8.RuneCountInString(detail.label)+1)
	}
	labelWidth = min(labelWidth, humanDetailLabelMax)
	valueWidth := humanLineWidth - labelWidth - humanColumnGap
	for _, detail := range details {
		label := truncateHumanText(detail.label+":", labelWidth)
		lines := wrapHumanText(detail.value, valueWidth)
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
	return strings.Join(strings.Fields(value), " ")
}

func truncateHumanText(value string, width int) string {
	value = sanitizeHumanText(value)
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func wrapHumanText(value string, width int) []string {
	value = sanitizeHumanText(value)
	if value == "" {
		return []string{"-"}
	}
	var lines []string
	for utf8.RuneCountInString(value) > width {
		runes := []rune(value)
		cut := width
		for index := width; index > 0; index-- {
			if runes[index-1] == ' ' {
				cut = index - 1
				break
			}
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
