package terminal

import "testing"

func TestScreenWritesAndSGR(t *testing.T) {
	s := NewScreen(10, 3)
	s.Write([]byte("hello\r\n\x1b[31mred\x1b[0m"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:5]); got != "hello" {
		t.Fatalf("row 0 prefix = %q", got)
	}
	row := frame.Row(1)
	if got := stringRunes(row[:3]); got != "red" {
		t.Fatalf("row 1 prefix = %q", got)
	}
	if row[0].Style.Fg.Mode != ColorIndexed || row[0].Style.Fg.Index != 1 {
		t.Fatalf("red style = %#v", row[0].Style.Fg)
	}
}

func TestSGRColonSubparameters(t *testing.T) {
	s := NewScreen(20, 2)
	// ITU colon form with colorspace id, plain colon form, and a colon
	// underline style; 58 (underline color) must not desync later params.
	s.Write([]byte("\x1b[38:2::255:10:0ma"))
	s.Write([]byte("\x1b[0m\x1b[38:2:1:2:3mb"))
	s.Write([]byte("\x1b[0m\x1b[4:3mc"))
	s.Write([]byte("\x1b[0m\x1b[4:0md"))
	s.Write([]byte("\x1b[0m\x1b[58:2::9:9:9;31me"))
	s.Write([]byte("\x1b[0m\x1b[38:5:196mf"))
	frame := s.Snapshot()
	defer frame.Release()

	row := frame.Row(0)
	if fg := row[0].Style.Fg; fg.Mode != ColorRGB || fg.R != 255 || fg.G != 10 || fg.B != 0 {
		t.Fatalf("38:2::R:G:B fg = %#v", fg)
	}
	if fg := row[1].Style.Fg; fg.Mode != ColorRGB || fg.R != 1 || fg.G != 2 || fg.B != 3 {
		t.Fatalf("38:2:R:G:B fg = %#v", fg)
	}
	if !row[2].Style.Has(AttrUnderline) {
		t.Fatalf("4:3 should enable underline: %#v", row[2].Style)
	}
	if row[3].Style.Has(AttrUnderline) {
		t.Fatalf("4:0 should disable underline: %#v", row[3].Style)
	}
	if fg := row[4].Style.Fg; fg.Mode != ColorIndexed || fg.Index != 1 {
		t.Fatalf("31 after 58:2::9:9:9 fg = %#v", fg)
	}
	if fg := row[5].Style.Fg; fg.Mode != ColorIndexed || fg.Index != 196 {
		t.Fatalf("38:5:196 fg = %#v", fg)
	}
}

func TestAlternateScreenReturnsToNormal(t *testing.T) {
	s := NewScreen(8, 2)
	s.Write([]byte("normal"))
	s.Write([]byte("\x1b[?1049halt\x1b[?1049l"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:6]); got != "normal" {
		t.Fatalf("normal buffer = %q", got)
	}
}

func TestRepeatPreviousCharacter(t *testing.T) {
	s := NewScreen(8, 1)
	s.Write([]byte("A \x1b[3bB"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:6]); got != "A    B" {
		t.Fatalf("row = %q, want %q", got, "A    B")
	}
}

func TestControlStringsAreIgnored(t *testing.T) {
	s := NewScreen(8, 1)
	s.Write([]byte("A\x1bP1;2|hidden\x1b\\B"))
	s.Write([]byte{'C', 0x90, 'h', 'i', 0x9c, 'D'})
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:4]); got != "ABCD" {
		t.Fatalf("row = %q, want %q", got, "ABCD")
	}
}

func TestC1CSI(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte{'A', 0x9b, '3', 'G', 'B'})
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)); got != "A B " {
		t.Fatalf("row = %q, want %q", got, "A B ")
	}
}

func TestAutowrapCanBeDisabled(t *testing.T) {
	s := NewScreen(3, 2)
	s.Write([]byte("ab\x1b[?7lcd"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)); got != "abd" {
		t.Fatalf("row 0 = %q, want %q", got, "abd")
	}
	if got := stringRunes(frame.Row(1)); got != "   " {
		t.Fatalf("row 1 = %q, want blank row", got)
	}
}

func TestPrivateCursorSaveRestore(t *testing.T) {
	s := NewScreen(5, 1)
	s.Write([]byte("ab\x1b[?1048h\x1b[1;1HZ\x1b[?1048lQ"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:3]); got != "ZbQ" {
		t.Fatalf("row = %q, want %q", got, "ZbQ")
	}
}

func TestOriginModeAddressesScrollRegion(t *testing.T) {
	s := NewScreen(5, 4)
	s.Write([]byte("\x1b[2;3r\x1b[?6h\x1b[1;1HX"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := frame.Row(0)[0].Rune(); got != ' ' {
		t.Fatalf("row 0 col 0 = %q, want blank", got)
	}
	if got := frame.Row(1)[0].Rune(); got != 'X' {
		t.Fatalf("row 1 col 0 = %q, want X", got)
	}
}

func TestHorizontalTabStop(t *testing.T) {
	s := NewScreen(10, 1)
	s.Write([]byte("\x1b[5G\x1bH\rA\tB"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := stringRunes(frame.Row(0)[:5]); got != "A   B" {
		t.Fatalf("row = %q, want %q", got, "A   B")
	}
}

func TestBlankWriteMarksVisibleCursorDirty(t *testing.T) {
	s := NewScreen(4, 1)
	if !s.Write([]byte(" ")) {
		t.Fatal("writing a blank over a blank cell should be dirty because the visible cursor moves")
	}
	frame := s.Snapshot()
	defer frame.Release()
	if frame.CursorX != 1 || frame.CursorY != 0 {
		t.Fatalf("cursor = %d,%d, want 1,0", frame.CursorX, frame.CursorY)
	}
}

func TestSoftResetResetsStyle(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte("\x1b[31mR\x1b[2G\x1b[!p\x1b[2GX"))
	frame := s.Snapshot()
	defer frame.Release()

	if frame.Row(0)[0].Style.Fg.Mode != ColorIndexed {
		t.Fatalf("first cell fg = %#v, want indexed red", frame.Row(0)[0].Style.Fg)
	}
	if frame.Row(0)[1].Style.Fg.Mode != ColorDefault {
		t.Fatalf("second cell fg = %#v, want default", frame.Row(0)[1].Style.Fg)
	}
}

func TestWideCharacterOccupiesTwoCells(t *testing.T) {
	s := NewScreen(5, 1)
	s.Write([]byte("你A"))
	frame := s.Snapshot()
	defer frame.Release()

	row := frame.Row(0)
	if row[0].Rune() != '你' || !row[0].Wide() {
		t.Fatalf("cell 0 = %#v, want wide 你", row[0])
	}
	if !row[1].WideContinuation() {
		t.Fatalf("cell 1 = %#v, want wide continuation", row[1])
	}
	if row[2].Rune() != 'A' {
		t.Fatalf("cell 2 = %q, want A", row[2].Rune())
	}
}

func TestCombiningCharacterStaysInPreviousCell(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte("e\u0301x"))
	frame := s.Snapshot()
	defer frame.Release()

	row := frame.Row(0)
	if row[0].Text() != "e\u0301" {
		t.Fatalf("cell 0 text = %q, want composed e", row[0].Text())
	}
	if row[1].Rune() != 'x' {
		t.Fatalf("cell 1 = %q, want x", row[1].Rune())
	}
}

func TestOverwritingWideContinuationClearsWideCell(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte("你\x1b[2Gx"))
	frame := s.Snapshot()
	defer frame.Release()

	row := frame.Row(0)
	if row[0].Rune() != ' ' || row[0].Wide() {
		t.Fatalf("cell 0 = %#v, want blank", row[0])
	}
	if row[1].Rune() != 'x' || row[1].WideContinuation() {
		t.Fatalf("cell 1 = %#v, want x", row[1])
	}
}

func TestRicherSGRAttributes(t *testing.T) {
	s := NewScreen(3, 1)
	s.Write([]byte("\x1b[3;5;8;9;53mX\x1b[23;25;28;29;55mY"))
	frame := s.Snapshot()
	defer frame.Release()

	first := frame.Row(0)[0].Style
	if !first.Has(AttrItalic) || !first.Has(AttrBlink) || !first.Has(AttrHidden) || !first.Has(AttrStrikethrough) || !first.Has(AttrOverline) {
		t.Fatalf("first style = %#v, want richer SGR attributes", first)
	}
	second := frame.Row(0)[1].Style
	if second.Has(AttrItalic | AttrBlink | AttrHidden | AttrStrikethrough | AttrOverline) {
		t.Fatalf("second style = %#v, want attributes reset", second)
	}
}

func TestInverseSGRCanBeResetBeforeClearing(t *testing.T) {
	s := NewScreen(4, 1)
	s.Write([]byte("\x1b[7mAB\x1b[27m\r\x1b[K"))
	frame := s.Snapshot()
	defer frame.Release()

	for col, cell := range frame.Row(0) {
		if cell.Style.Has(AttrInverse) {
			t.Fatalf("cell %d still inverse after reset and clear: %#v", col, cell)
		}
		if cell.Rune() != ' ' {
			t.Fatalf("cell %d = %q, want blank", col, cell.Rune())
		}
	}
}

func stringRunes(cells []Cell) string {
	runes := make([]rune, len(cells))
	for i, cell := range cells {
		runes[i] = cell.Rune()
	}
	return string(runes)
}
