package svg

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rabarbra/ttysvg/internal/terminal"
)

func TestRendererPositionsRunsAfterSpaces(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{
		Cols:       5,
		Rows:       1,
		Theme:      "dark",
		FontSize:   10,
		CellWidth:  10,
		CellHeight: 12,
	})

	frame := terminal.Frame{
		Cols: 5,
		Rows: 1,
		Data: []terminal.Cell{
			{Ch: 'A'},
			terminal.BlankCell(),
			terminal.BlankCell(),
			terminal.BlankCell(),
			{Ch: 'B'},
		},
	}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.Contains(out, ">A   B</text>") {
		t.Fatalf("renderer emitted spaced cells as one text run:\n%s", out)
	}
	if !strings.Contains(out, `<text x="0" y="10" class="fg">A</text>`) {
		t.Fatalf("missing A at cell 0:\n%s", out)
	}
	if !strings.Contains(out, `<text x="40" y="10" class="fg">B</text>`) {
		t.Fatalf("missing B at cell 4:\n%s", out)
	}
}

func TestRendererPinsEveryCellInTextRun(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{
		Cols:       3,
		Rows:       1,
		Theme:      "dark",
		FontSize:   10,
		CellWidth:  10,
		CellHeight: 12,
	})

	frame := terminal.Frame{
		Cols: 3,
		Rows: 1,
		Data: []terminal.Cell{
			{Ch: '╭'},
			{Ch: '─'},
			{Ch: '╮'},
		},
	}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `<text x="0 10 20" y="10" class="fg">╭─╮</text>`) {
		t.Fatalf("text run was not pinned to every cell:\n%s", out)
	}
}

func TestRendererCanMinify(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12, Minify: true})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), "\n") {
		t.Fatalf("minified SVG contains newline:\n%s", buf.String())
	}
}

func TestRendererEmitsCursor(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}}, CursorVisible: true}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `<rect x="0" y="0" width="10" height="12" class="fg"/>`) {
		t.Fatalf("missing cursor rectangle:\n%s", out)
	}
	if !strings.Contains(out, `<text x="0" y="10" class="bg">A</text>`) {
		t.Fatalf("missing cursor text overlay:\n%s", out)
	}
}

func TestRendererFinalBlankRowCoversPreviousText(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 3, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	full := terminal.Frame{Cols: 3, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}, {Ch: 'B'}, {Ch: 'C'}}}
	blank := terminal.Frame{Cols: 3, Rows: 1, Data: []terminal.Cell{terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell()}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFrame(full, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(blank, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `<set attributeName="visibility" to="visible" begin="1s" fill="freeze"/>`) {
		t.Fatalf("missing final blank row group:\n%s", out)
	}
	if !strings.Contains(out, `<rect x="0" y="0" width="30" height="12" class="bg"/>`) {
		t.Fatalf("missing final row background cover:\n%s", out)
	}
}

func TestRendererEmitsNonFinalBlankRows(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 4, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	menu := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{{Ch: 'm'}, {Ch: 'e'}, {Ch: 'n'}, {Ch: 'u'}}}
	blank := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell()}}
	reused := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{{Ch: 'd'}, {Ch: 'o'}, {Ch: 'n'}, {Ch: 'e'}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFrame(menu, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFrame(blank, time.Second, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(reused, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	blankGroup := `<set attributeName="visibility" to="visible" begin="1s" dur="1s"/>`
	rowClear := `<rect x="0" y="0" width="40" height="12" class="bg"/>`
	if !strings.Contains(out, blankGroup) {
		t.Fatalf("missing non-final blank row interval:\n%s", out)
	}
	if strings.Count(out, rowClear) < 3 {
		t.Fatalf("expected row clear for menu, blank, and final rows; found %d:\n%s", strings.Count(out, rowClear), out)
	}
}

func TestRendererClearsRowBeforeInverseHighlight(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 2, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame1 := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{{Ch: 'A', Style: terminal.Style{Inverse: true}}, {Ch: 'B'}}}
	frame2 := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}, {Ch: 'B', Style: terminal.Style{Inverse: true}}}}
	frame3 := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}, {Ch: 'B'}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFrame(frame1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFrame(frame2, time.Second, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame3, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	rowClear := `<rect x="0" y="0" width="20" height="12" class="bg"/>`
	if strings.Count(out, rowClear) < 3 {
		t.Fatalf("expected every emitted row to clear first; found %d clears:\n%s", strings.Count(out, rowClear), out)
	}
	if !strings.Contains(out, `<rect x="10" y="0" width="10" height="12" class="fg"/>`) {
		t.Fatalf("missing moved inverse highlight:\n%s", out)
	}
}

func TestRendererEmitsCombiningAndSkipsWideContinuation(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 4, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{
		{Ch: 'e', Combining: "\u0301"},
		{Ch: '你', Wide: true},
		{Ch: ' ', WideContinuation: true},
		{Ch: 'x'},
	}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "e\u0301你") {
		t.Fatalf("missing combining text:\n%s", out)
	}
	if strings.Contains(out, `<text x="10 20"`) {
		t.Fatalf("wide continuation received its own x position:\n%s", out)
	}
}

func TestRendererEmitsRicherTextAttributes(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A', Style: terminal.Style{Italic: true, Underline: true, Strikethrough: true, Overline: true}}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, 0); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `font-style="italic"`) || !strings.Contains(out, `text-decoration="underline line-through overline"`) {
		t.Fatalf("missing richer text attributes:\n%s", out)
	}
}
