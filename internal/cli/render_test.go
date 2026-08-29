package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHumanRenderersBoundEveryLine(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	app := &application{stdout: &stdout}
	longValue := strings.Repeat("very-long-value-", 40)
	if err := app.writeHumanTable(
		[]humanColumn{{heading: "FIRST", width: 20}, {heading: "SECOND", width: 98}},
		[][]string{{longValue, longValue}},
	); err != nil {
		t.Fatal(err)
	}
	if err := app.writeWrappedHumanTable(
		[]humanColumn{{heading: "FIRST", width: 20}, {heading: "SECOND", width: 98}},
		[][]string{{longValue, longValue}},
	); err != nil {
		t.Fatal(err)
	}
	if err := app.writeHumanDetails([]humanDetail{{label: "Long value", value: longValue}}); err != nil {
		t.Fatal(err)
	}
	assertHumanLinesBounded(t, stdout.String())
	if !strings.Contains(stdout.String(), "…") {
		t.Fatalf("compact table did not mark truncation: %q", stdout.String())
	}
}

func TestGoPrettyStyleRendering(t *testing.T) {
	t.Parallel()
	app := &application{}
	var out bytes.Buffer
	app.stdout = &out
	cols := []humanColumn{
		{heading: "ID", width: 10},
		{heading: "Agent", width: 10},
		{heading: "Status", width: 10},
	}
	rows := [][]string{
		{"omp-1234", "omp", "running"},
		{"pi-5678", "pi", "idle"},
	}
	if err := app.writeHumanTable(cols, rows); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	if strings.Contains(output, "─") || strings.Contains(output, "---") {
		t.Fatalf("borderless table contains border characters: %q", output)
	}
}

func assertHumanLinesBounded(t *testing.T, output string) {
	t.Helper()
	for number, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if width := utf8.RuneCountInString(line); width > humanLineWidth {
			t.Fatalf("line %d width = %d, want <= %d: %q", number+1, width, humanLineWidth, line)
		}
	}
}
