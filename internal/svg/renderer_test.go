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
	if !strings.Contains(out, `<text x="0" y="10">A</text>`) {
		t.Fatalf("missing A at cell 0:\n%s", out)
	}
	if !strings.Contains(out, `<text x="40" y="10">B</text>`) {
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
	if !strings.Contains(out, `<text x="0 10 20" y="10">╭─╮</text>`) {
		t.Fatalf("text run was not pinned to every cell:\n%s", out)
	}
}

func TestRendererOutputHasNoNewlines(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
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
		t.Fatalf("SVG output contains newline:\n%s", buf.String())
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
	if !strings.Contains(out, `<rect x="0" y="0" width="10" height="12"/>`) {
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
	if !strings.Contains(out, `<set attributeName="opacity" to="1" begin="1s" fill="freeze"/>`) {
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
	blankGroup := `<set attributeName="opacity" to="1" begin="1s" dur="1s"/>`
	rowClear := `<rect x="0" y="0" width="40" height="12" class="bg"/>`
	if !strings.Contains(out, blankGroup) {
		t.Fatalf("missing non-final blank row interval:\n%s", out)
	}
	if strings.Count(out, rowClear) < 3 {
		t.Fatalf("expected row clear for menu, blank, and final rows; found %d:\n%s", strings.Count(out, rowClear), out)
	}
}

func TestRendererLoopsWithIndependentAnimations(t *testing.T) {
	var buf bytes.Buffer
	// TotalDuration 1s -> period = 1s + loopEndHold(2s) = 3s.
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12, Loop: true, TotalDuration: time.Second})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteFinalFrame(frame, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// No shared timebase or syncbase references — each reveal repeats on its own.
	if strings.Contains(out, `tb.begin`) || strings.Contains(out, `id="tb"`) {
		t.Fatalf("loop should not use a shared timebase:\n%s", out)
	}
	if strings.Contains(out, `fill="freeze"`) {
		t.Fatalf("frozen reveal would not loop:\n%s", out)
	}
	// Reveals are independent discrete animations repeating every period (3s).
	if !strings.Contains(out, `<animate attributeName="opacity" calcMode="discrete" dur="3s" repeatCount="indefinite"`) {
		t.Fatalf("missing independent looping animation:\n%s", out)
	}
	// The final 'A' reveal becomes visible at 1s/3s and holds to the loop end.
	if !strings.Contains(out, `values="0;1" keyTimes="0;0.333333"`) {
		t.Fatalf("final reveal timing wrong:\n%s", out)
	}
}

func TestRendererPromotesRepeatedColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	const col = "#79808f" // not one of the 16 palette colors, fg, or bg

	for i := 0; i < dynPromoteAt-1; i++ {
		if got := r.fillToken(col); got != ` fill="`+col+`"` {
			t.Fatalf("use %d: got %q, want inline fill", i+1, got)
		}
	}
	if got := r.fillToken(col); got != ` class="c0"` {
		t.Fatalf("promotion: got %q, want class reference", got)
	}
	if got := r.fillToken(col); got != ` class="c0"` {
		t.Fatalf("after promotion: got %q, want class reference", got)
	}
	if !strings.Contains(r.dynStyle.String(), ".c0{fill:"+col+"}") {
		t.Fatalf("missing dynamic class definition: %q", r.dynStyle.String())
	}
}

func TestRendererKeepsRareColorInline(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	const col = "#123456"
	for i := 0; i < dynPromoteAt-1; i++ {
		if got := r.fillToken(col); got != ` fill="`+col+`"` {
			t.Fatalf("rare color use %d should stay inline, got %q", i+1, got)
		}
	}
	if r.dynStyle.Len() != 0 {
		t.Fatalf("rare color should not be promoted, got defs %q", r.dynStyle.String())
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
	if !strings.Contains(out, `<rect x="10" y="0" width="10" height="12"/>`) {
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
