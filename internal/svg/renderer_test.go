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
	// A short space gap is bridged into one run. Gap cells appear in neither
	// the x list nor the glyph string (space glyphs would be collapsed by
	// WebKit's whitespace handling and shift every later glyph), so B is
	// pinned directly to cell 4's x.
	if !strings.Contains(out, `<text x="0 40" y="10">AB</text>`) {
		t.Fatalf("spaced cells not merged into one pinned run:\n%s", out)
	}
}

func TestRendererSplitsRunsAtLongGaps(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{
		Cols:       textGapMax + 3,
		Rows:       1,
		Theme:      "dark",
		FontSize:   10,
		CellWidth:  10,
		CellHeight: 12,
	})

	cells := make([]terminal.Cell, textGapMax+3)
	for i := range cells {
		cells[i] = terminal.BlankCell()
	}
	cells[0].Ch = 'A'
	cells[len(cells)-1].Ch = 'B'
	frame := terminal.Frame{Cols: len(cells), Rows: 1, Data: cells}

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
	if !strings.Contains(out, `<text x="0" y="10">A</text>`) {
		t.Fatalf("run should split at a gap wider than textGapMax:\n%s", out)
	}
}

func TestRendererDoesNotBridgeDecoratedRuns(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 3, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	underlined := terminal.Style{Attrs: terminal.AttrUnderline}
	frame := terminal.Frame{Cols: 3, Rows: 1, Data: []terminal.Cell{
		{Ch: 'A', Style: underlined},
		terminal.BlankCell(),
		{Ch: 'B', Style: underlined},
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
	// Bridging would paint the underline across the undecorated space.
	if !strings.Contains(out, `>A</text>`) || !strings.Contains(out, `>B</text>`) {
		t.Fatalf("decorated cells must not bridge across the plain gap:\n%s", out)
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

func TestRendererAppendsFontFallbacks(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, FontFamily: "'My Terminal Font'"})
	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "My Terminal Font") || !strings.Contains(out, "monospace") {
		t.Fatalf("custom font should keep the default fallback stack:\n%s", out)
	}
	// A stack already ending in a generic family is left alone.
	if got := fontFamilyWithFallback("Menlo, monospace"); got != "Menlo, monospace" {
		t.Fatalf("fontFamilyWithFallback double-appended: %q", got)
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

func TestRendererStaticSnapshotHasNoAnimation(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12, Static: true})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}}}

	if err := renderer.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.WriteStaticFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := renderer.End(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.Contains(out, `<animate`) || strings.Contains(out, `<set `) || strings.Contains(out, `opacity="0"`) {
		t.Fatalf("static snapshot should not contain animation markup:\n%s", out)
	}
	if !strings.Contains(out, `<title>Terminal snapshot</title>`) || !strings.Contains(out, `>A</text>`) {
		t.Fatalf("static snapshot missing expected content:\n%s", out)
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
	// The earlier text hides because its own reveal interval ends at 1s; the
	// blank final state needs no cover rect (and emits no group at all).
	if !strings.Contains(out, `<set attributeName="opacity" to="1" begin="0s" dur="1s"/>`) {
		t.Fatalf("previous text reveal should end when the row goes blank:\n%s", out)
	}
	if strings.Contains(out, `class="bg"/>`) {
		t.Fatalf("blank rows should not emit background cover rects:\n%s", out)
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
	// The menu text is revealed for exactly [0s,1s); the blank state emits no
	// group of its own, and the final "done" text starts at 2s.
	if !strings.Contains(out, `<set attributeName="opacity" to="1" begin="0s" dur="1s"/>`) {
		t.Fatalf("menu reveal should end when the row goes blank:\n%s", out)
	}
	if !strings.Contains(out, `<set attributeName="opacity" to="1" begin="2s" fill="freeze"/>`) {
		t.Fatalf("missing final text reveal:\n%s", out)
	}
	if got := strings.Count(out, `<g opacity="0">`); got != 2 {
		t.Fatalf("expected 2 row groups (menu, done) with none for the blank state, got %d:\n%s", got, out)
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
	frame1 := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{{Ch: 'A', Style: terminal.Style{Attrs: terminal.AttrInverse}}, {Ch: 'B'}}}
	frame2 := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{{Ch: 'A'}, {Ch: 'B', Style: terminal.Style{Attrs: terminal.AttrInverse}}}}
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
	// Each highlight state is revealed only for its own interval; no cover
	// rects are needed because the intervals do not overlap.
	if !strings.Contains(out, `<rect x="0" y="0" width="10" height="12"/>`) {
		t.Fatalf("missing initial inverse highlight:\n%s", out)
	}
	if !strings.Contains(out, `<rect x="10" y="0" width="10" height="12"/>`) {
		t.Fatalf("missing moved inverse highlight:\n%s", out)
	}
	if strings.Contains(out, `class="bg"/>`) {
		t.Fatalf("row cover rects should no longer be emitted:\n%s", out)
	}
}

func TestRendererEmitsCombiningAndSkipsWideContinuation(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 4, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{
		{Ch: 'e', Combining: "\u0301"},
		{Ch: '你', Style: terminal.Style{Attrs: terminal.AttrWide}},
		{Ch: ' ', Style: terminal.Style{Attrs: terminal.AttrWideContinuation}},
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
	// The combining sequence is more than one UTF-16 code unit, so it must end
	// its run (Gecko consumes one x entry per code unit); the following BMP
	// cells continue in their own pinned run.
	if !strings.Contains(out, "e\u0301</text>") {
		t.Fatalf("missing combining text run:\n%s", out)
	}
	if !strings.Contains(out, `<text x="10 30" y="10">你x</text>`) {
		t.Fatalf("run after combining cell not pinned correctly:\n%s", out)
	}
	if strings.Contains(out, `<text x="10 20"`) {
		t.Fatalf("wide continuation received its own x position:\n%s", out)
	}
}

func TestRendererEmitsRicherTextAttributes(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 1, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 1, Rows: 1, Data: []terminal.Cell{{Ch: 'A', Style: terminal.Style{Attrs: terminal.AttrItalic | terminal.AttrUnderline | terminal.AttrStrikethrough | terminal.AttrOverline}}}}

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

func TestRendererEndsRunAfterSupplementaryRune(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 5, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 5, Rows: 1, Data: []terminal.Cell{
		{Ch: '🐌', Style: terminal.Style{Attrs: terminal.AttrWide}},
		{Ch: ' ', Style: terminal.Style{Attrs: terminal.AttrWideContinuation}},
		{Ch: 'E'},
		{Ch: 'T'},
		{Ch: 'A'},
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
	// A supplementary-plane rune is two UTF-16 code units; Gecko consumes two
	// x entries for it, so any glyph sharing its run would shift. It must end
	// the run, with the following text pinned in its own run.
	if !strings.Contains(out, `<text x="0" y="10">🐌</text>`) {
		t.Fatalf("supplementary rune should end its own run:\n%s", out)
	}
	if !strings.Contains(out, `<text x="20 30 40" y="10">ETA</text>`) {
		t.Fatalf("text after supplementary rune should start a new pinned run:\n%s", out)
	}
}

func TestRendererDrawsBlockElementsAsRects(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 4, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	fg := terminal.Style{Fg: terminal.Color{Mode: terminal.ColorRGB, R: 0xeb, G: 0xcb, B: 0x8b}}
	frame := terminal.Frame{Cols: 4, Rows: 1, Data: []terminal.Cell{
		{Ch: '█', Style: fg},
		{Ch: '█', Style: fg},
		{Ch: '░', Style: fg},
		{Ch: '░', Style: fg},
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
	// Block cells render as exact rects (font fallbacks draw them with the
	// wrong ink coverage); identical adjacent cells merge into one rect and
	// shades carry their coverage as fill-opacity.
	if !strings.Contains(out, `<rect x="0" y="0" width="20" height="12" fill="#ebcb8b"/>`) {
		t.Fatalf("solid blocks should merge into one full-cell rect:\n%s", out)
	}
	if !strings.Contains(out, `<rect x="20" y="0" width="20" height="12" fill="#ebcb8b" fill-opacity="0.25"/>`) {
		t.Fatalf("light shade should be a rect with 25%% fill-opacity:\n%s", out)
	}
	if strings.Contains(out, "█") || strings.Contains(out, "░") {
		t.Fatalf("block runes should not be emitted as text glyphs:\n%s", out)
	}
}

func TestRendererDrawsLowerBlockAnchoredToCellBottom(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, Config{Cols: 2, Rows: 1, Theme: "dark", FontSize: 10, CellWidth: 10, CellHeight: 12})
	frame := terminal.Frame{Cols: 2, Rows: 1, Data: []terminal.Cell{
		{Ch: '▃'}, // lower three-eighths
		{Ch: '▃'},
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
	// 3/8 of the 12px cell, anchored at the bottom: y = round(12*5/8) = 8.
	if !strings.Contains(out, `<rect x="0" y="8" width="20" height="4"/>`) {
		t.Fatalf("lower block should be a bottom-anchored rect inheriting the default fill:\n%s", out)
	}
}
