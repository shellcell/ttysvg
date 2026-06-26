package terminal

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

type Screen struct {
	cols int
	rows int

	normal    buffer
	alternate buffer
	active    *buffer

	style             Style
	cursorVisible     bool
	autoWrap          bool
	originMode        bool
	applicationCursor bool
	lineDrawing       bool
	parser            parserState
	csi               [256]byte
	csiLen            int
	utf8Buf           [utf8.UTFMax]byte
	utf8Len           int
	lastCell          Cell
	lastWidth         int
	hasLastCell       bool
	terminfo          Terminfo
	tabStops          []bool
}

type buffer struct {
	cells        []Cell
	x            int
	y            int
	wrap         bool
	savedX       int
	savedY       int
	scrollTop    int
	scrollBottom int
}

type parserState uint8

const (
	stateGround parserState = iota
	stateEsc
	stateCSI
	stateOSC
	stateOSCEsc
	stateString
	stateStringEsc
	stateCharset
	stateHash
)

func NewScreen(cols, rows int) *Screen {
	s := &Screen{cols: cols, rows: rows, cursorVisible: true, autoWrap: true, tabStops: defaultTabStops(cols)}
	s.normal = newBuffer(cols, rows)
	s.alternate = newBuffer(cols, rows)
	s.active = &s.normal
	return s
}

func newBuffer(cols, rows int) buffer {
	b := buffer{
		cells:        make([]Cell, cols*rows),
		scrollBottom: rows - 1,
	}
	fillCells(b.cells, BlankCell())
	return b
}

func (s *Screen) Snapshot() Frame {
	box, cells := acquireCells(len(s.active.cells))
	copy(cells, s.active.cells)
	return Frame{Cols: s.cols, Rows: s.rows, Data: cells, CursorX: s.active.x, CursorY: s.active.y, CursorVisible: s.cursorVisible, box: box}
}

func (s *Screen) AlternateActive() bool {
	return s.active == &s.alternate
}

// ApplicationCursorKeys reports whether the child has enabled DECCKM, in which
// case the arrow/Home/End keys are expected to send ESC O x instead of ESC [ x.
func (s *Screen) ApplicationCursorKeys() bool {
	return s.applicationCursor
}

func (s *Screen) SetTerminfo(info Terminfo) {
	s.terminfo = info
}

func (s *Screen) Write(data []byte) bool {
	dirty := false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if s.parser == stateGround && s.utf8Len == 0 {
			if consumed, changed, ok := s.handleTerminfo(data[i:]); ok {
				if changed {
					dirty = true
				}
				i += consumed - 1
				continue
			}
		}

		if s.parser == stateGround && s.utf8Len > 0 {
			s.utf8Buf[s.utf8Len] = b
			s.utf8Len++
			if r, size, ok := decodeBufferedRune(s.utf8Buf[:s.utf8Len]); ok {
				s.utf8Len = 0
				if s.putRune(r) {
					dirty = true
				}
			} else if size >= utf8.UTFMax {
				s.utf8Len = 0
				if s.putRune(utf8.RuneError) {
					dirty = true
				}
			}
			continue
		}

		switch s.parser {
		case stateGround:
			if s.handleGroundByte(data, &i, b) {
				dirty = true
			}
		case stateEsc:
			if s.handleEsc(b) {
				dirty = true
			}
		case stateCSI:
			if s.handleCSIByte(b) {
				dirty = true
			}
		case stateOSC:
			if b == 0x07 || b == 0x9c {
				s.parser = stateGround
			} else if b == 0x1b {
				s.parser = stateOSCEsc
			}
		case stateOSCEsc:
			if b == '\\' {
				s.parser = stateGround
			} else if b == 0x1b {
				s.parser = stateOSCEsc
			} else {
				s.parser = stateOSC
			}
		case stateString:
			if b == 0x07 || b == 0x9c {
				s.parser = stateGround
			} else if b == 0x1b {
				s.parser = stateStringEsc
			}
		case stateStringEsc:
			if b == '\\' {
				s.parser = stateGround
			} else if b == 0x1b {
				s.parser = stateStringEsc
			} else {
				s.parser = stateString
			}
		case stateCharset:
			s.lineDrawing = b == '0'
			s.parser = stateGround
		case stateHash:
			s.parser = stateGround
			if b == '8' {
				s.alignmentTest()
				dirty = true
			}
		}
	}
	return dirty
}

func (s *Screen) handleTerminfo(data []byte) (int, bool, bool) {
	for _, fixed := range s.terminfo.fixed {
		if len(fixed.seq) > 0 && len(data) >= len(fixed.seq) && bytes.Equal(data[:len(fixed.seq)], fixed.seq) {
			return len(fixed.seq), s.applyTerminfoAction(fixed.action), true
		}
	}
	if n, x, y, ok := s.terminfo.cup.match(data); ok {
		return n, s.moveCursorTo(y, x), true
	}
	return 0, false, false
}

func (s *Screen) applyTerminfoAction(action terminfoAction) bool {
	switch action {
	case terminfoClear:
		s.eraseDisplay(2)
		return s.moveCursor(0, 0) || true
	case terminfoEraseDisplay:
		s.eraseDisplay(0)
		return true
	case terminfoEraseLine:
		s.eraseLine(0)
		return true
	case terminfoEraseLineLeft:
		s.eraseLine(1)
		return true
	case terminfoEnterAlt:
		s.saveCursor()
		return s.setAlternate(true, true)
	case terminfoExitAlt:
		dirty := s.setAlternate(false, false)
		if s.restoreCursor() {
			dirty = true
		}
		return dirty
	case terminfoCursorInvisible:
		if !s.cursorVisible {
			return false
		}
		s.cursorVisible = false
		return true
	case terminfoCursorNormal:
		if s.cursorVisible {
			return false
		}
		s.cursorVisible = true
		return true
	case terminfoSaveCursor:
		s.saveCursor()
		return false
	case terminfoRestoreCursor:
		return s.restoreCursor()
	case terminfoHome:
		return s.moveCursorTo(0, 0)
	case terminfoLastLine:
		return s.moveCursor(0, s.rows-1)
	default:
		return false
	}
}

func (s *Screen) handleGroundByte(data []byte, i *int, b byte) bool {
	switch b {
	case 0x00, 0x7f:
		return false
	case 0x07:
		return false
	case 0x08:
		return s.moveCursor(s.active.x-1, s.active.y)
	case 0x09:
		return s.moveCursor(s.nextTab(s.active.x, 1), s.active.y)
	case 0x0a, 0x0b, 0x0c:
		s.lineFeed()
		return true
	case 0x0d:
		return s.moveCursor(0, s.active.y)
	case 0x1b:
		s.parser = stateEsc
		return false
	case 0x84:
		s.lineFeed()
		return true
	case 0x85:
		s.active.x = 0
		s.lineFeed()
		return true
	case 0x88:
		s.setTabStop()
		return false
	case 0x8d:
		s.reverseIndex()
		return true
	case 0x8e, 0x8f:
		return false
	case 0x90, 0x98, 0x9e, 0x9f:
		s.parser = stateString
		return false
	case 0x9b:
		s.csiLen = 0
		s.parser = stateCSI
		return false
	case 0x9c:
		return false
	case 0x9d:
		s.parser = stateOSC
		return false
	}

	if b < utf8.RuneSelf {
		return s.putRune(rune(b))
	}
	if utf8.FullRune(data[*i:]) {
		r, size := utf8.DecodeRune(data[*i:])
		*i += size - 1
		return s.putRune(r)
	}
	copy(s.utf8Buf[:], data[*i:])
	s.utf8Len = len(data[*i:])
	*i = len(data)
	return false
}

func (s *Screen) handleEsc(b byte) bool {
	s.parser = stateGround
	switch b {
	case '[':
		s.csiLen = 0
		s.parser = stateCSI
	case ']':
		s.parser = stateOSC
	case 'P', '^', '_', 'X':
		s.parser = stateString
	case '(', ')', '*', '+':
		s.parser = stateCharset
	case '#':
		s.parser = stateHash
	case '7':
		s.saveCursor()
	case '8':
		return s.restoreCursor()
	case 'H':
		s.setTabStop()
	case 'D':
		s.lineFeed()
		return true
	case 'E':
		s.active.x = 0
		s.lineFeed()
		return true
	case 'M':
		s.reverseIndex()
		return true
	case 'c':
		s.reset()
		return true
	}
	return false
}

func (s *Screen) handleCSIByte(b byte) bool {
	if b >= 0x40 && b <= 0x7e {
		raw := s.csi[:s.csiLen]
		s.parser = stateGround
		return s.handleCSI(raw, b)
	}
	if s.csiLen < len(s.csi) {
		s.csi[s.csiLen] = b
		s.csiLen++
	}
	return false
}

func (s *Screen) handleCSI(raw []byte, final byte) bool {
	params := parseParams(raw)
	switch final {
	case 'A':
		return s.moveCursor(s.active.x, s.active.y-params.value(0, 1))
	case 'b':
		return s.repeatPrevious(params.value(0, 1))
	case 'B':
		return s.moveCursor(s.active.x, s.active.y+params.value(0, 1))
	case 'C', 'a':
		return s.moveCursor(s.active.x+params.value(0, 1), s.active.y)
	case 'D':
		return s.moveCursor(s.active.x-params.value(0, 1), s.active.y)
	case 'E':
		return s.moveCursor(0, s.active.y+params.value(0, 1))
	case 'F':
		return s.moveCursor(0, s.active.y-params.value(0, 1))
	case 'G', '`':
		return s.moveCursor(params.value(0, 1)-1, s.active.y)
	case 'H', 'f':
		return s.moveCursorTo(params.value(0, 1)-1, params.value(1, 1)-1)
	case 'I':
		return s.moveCursor(s.nextTab(s.active.x, params.value(0, 1)), s.active.y)
	case 'd':
		return s.moveCursor(s.active.x, params.value(0, 1)-1)
	case 'e':
		return s.moveCursor(s.active.x, s.active.y+params.value(0, 1))
	case 'J':
		s.eraseDisplay(params.value(0, 0))
		return true
	case 'K':
		s.eraseLine(params.value(0, 0))
		return true
	case 'X':
		s.eraseChars(params.value(0, 1))
		return true
	case 'L':
		s.insertLines(params.value(0, 1))
		return true
	case 'M':
		s.deleteLines(params.value(0, 1))
		return true
	case '@':
		s.insertChars(params.value(0, 1))
		return true
	case 'P':
		s.deleteChars(params.value(0, 1))
		return true
	case 'S':
		s.scrollUp(s.active.scrollTop, s.active.scrollBottom, params.value(0, 1))
		return true
	case 'T':
		s.scrollDown(s.active.scrollTop, s.active.scrollBottom, params.value(0, 1))
		return true
	case 'Z':
		return s.moveCursor(s.previousTab(s.active.x, params.value(0, 1)), s.active.y)
	case 'g':
		s.clearTab(params.value(0, 0))
	case 'm':
		s.applySGR(params)
	case 'p':
		if params.private == '!' {
			return s.softReset()
		}
	case 'r':
		s.setScrollRegion(params)
		return true
	case 's':
		s.saveCursor()
	case 'u':
		return s.restoreCursor()
	case 'h':
		return s.setModes(params, true)
	case 'l':
		return s.setModes(params, false)
	}
	return false
}

func (s *Screen) putRune(r rune) bool {
	if r < ' ' || r == utf8.RuneError {
		if r == utf8.RuneError {
			r = '?'
		} else {
			return false
		}
	}
	if isCombining(r) {
		return s.addCombining(r)
	}
	if s.lineDrawing && r < utf8.RuneSelf {
		r = decLineRune(byte(r))
	}
	width := runeWidth(r)
	cell := Cell{Ch: r, Wide: width == 2, Style: s.style}
	return s.putCellWidth(cell, width)
}

func (s *Screen) putCell(cell Cell) bool {
	width := 1
	if cell.Wide {
		width = 2
	}
	return s.putCellWidth(cell, width)
}

func (s *Screen) putCellWidth(cell Cell, width int) bool {
	if width < 1 {
		width = 1
	}
	if s.active.wrap {
		s.active.x = 0
		s.lineFeed()
		s.active.wrap = false
	}
	if width == 2 && s.active.x == s.cols-1 {
		if s.autoWrap {
			s.active.x = 0
			s.lineFeed()
		} else {
			width = 1
			cell.Wide = false
		}
	}

	dirty := false
	if s.clearWideAt(s.active.x, s.active.y) {
		dirty = true
	}
	if width == 2 && s.clearWideAt(s.active.x+1, s.active.y) {
		dirty = true
	}
	idx := s.active.y*s.cols + s.active.x
	if s.active.cells[idx] != cell {
		dirty = true
	}
	s.active.cells[idx] = cell
	s.lastCell = cell
	s.lastWidth = width
	s.hasLastCell = true
	if width == 2 && s.active.x+1 < s.cols {
		cont := Cell{Ch: ' ', WideContinuation: true, Style: s.style}
		if s.active.cells[idx+1] != cont {
			dirty = true
		}
		s.active.cells[idx+1] = cont
	}

	s.active.x += width
	if s.active.x >= s.cols {
		s.active.x = s.cols - 1
		if !s.autoWrap {
			return dirty
		}
		s.active.wrap = true
	}
	return dirty
}

func (s *Screen) addCombining(r rune) bool {
	idx, ok := s.lastCellIndex()
	if !ok {
		return false
	}
	cell := s.active.cells[idx]
	if cell.WideContinuation {
		return false
	}
	cell.Combining += string(r)
	if s.active.cells[idx] == cell {
		return false
	}
	s.active.cells[idx] = cell
	s.lastCell = cell
	s.hasLastCell = true
	return true
}

func (s *Screen) lastCellIndex() (int, bool) {
	if s.hasLastCell {
		x := s.active.x - s.lastWidth
		y := s.active.y
		if s.active.wrap {
			x = s.cols - s.lastWidth
		}
		if x >= 0 && x < s.cols && y >= 0 && y < s.rows {
			return y*s.cols + x, true
		}
	}
	if s.active.x > 0 {
		idx := s.active.y*s.cols + s.active.x - 1
		if s.active.cells[idx].WideContinuation && s.active.x > 1 {
			idx--
		}
		return idx, true
	}
	if s.active.y > 0 {
		idx := (s.active.y-1)*s.cols + s.cols - 1
		if s.active.cells[idx].WideContinuation && s.cols > 1 {
			idx--
		}
		return idx, true
	}
	return 0, false
}

func (s *Screen) clearWideAt(x, y int) bool {
	if x < 0 || x >= s.cols || y < 0 || y >= s.rows {
		return false
	}
	idx := y*s.cols + x
	dirty := false
	blank := Cell{Ch: ' ', Style: s.style}
	if s.active.cells[idx].WideContinuation && x > 0 && s.active.cells[idx-1].Wide {
		if s.active.cells[idx-1] != blank {
			s.active.cells[idx-1] = blank
			dirty = true
		}
	}
	if s.active.cells[idx].Wide && x+1 < s.cols && s.active.cells[idx+1].WideContinuation {
		if s.active.cells[idx+1] != blank {
			s.active.cells[idx+1] = blank
			dirty = true
		}
	}
	return dirty
}

func (s *Screen) repeatPrevious(n int) bool {
	if n <= 0 {
		n = 1
	}
	if !s.hasLastCell {
		return false
	}
	dirty := false
	for i := 0; i < n; i++ {
		if s.putCell(s.lastCell) {
			dirty = true
		}
	}
	return dirty
}

func (s *Screen) lineFeed() {
	s.active.wrap = false
	if s.active.y == s.active.scrollBottom {
		s.scrollUp(s.active.scrollTop, s.active.scrollBottom, 1)
		return
	}
	if s.active.y < s.rows-1 {
		s.active.y++
	}
}

func (s *Screen) reverseIndex() {
	s.active.wrap = false
	if s.active.y == s.active.scrollTop {
		s.scrollDown(s.active.scrollTop, s.active.scrollBottom, 1)
		return
	}
	if s.active.y > 0 {
		s.active.y--
	}
}

func (s *Screen) moveCursor(x, y int) bool {
	x = clamp(x, 0, s.cols-1)
	y = clamp(y, 0, s.rows-1)
	changed := s.active.x != x || s.active.y != y || s.active.wrap
	s.active.x = x
	s.active.y = y
	s.active.wrap = false
	return changed
}

func (s *Screen) moveCursorTo(row, col int) bool {
	if s.originMode {
		row = clamp(s.active.scrollTop+row, s.active.scrollTop, s.active.scrollBottom)
	}
	return s.moveCursor(col, row)
}

func (s *Screen) saveCursor() {
	s.active.savedX = s.active.x
	s.active.savedY = s.active.y
}

func (s *Screen) restoreCursor() bool {
	return s.moveCursor(s.active.savedX, s.active.savedY)
}

func (s *Screen) eraseDisplay(mode int) {
	blank := Cell{Ch: ' ', Style: s.style}
	s.active.wrap = false
	switch mode {
	case 1:
		fillCells(s.active.cells[:s.active.y*s.cols+s.active.x+1], blank)
	case 2, 3:
		fillCells(s.active.cells, blank)
	default:
		fillCells(s.active.cells[s.active.y*s.cols+s.active.x:], blank)
	}
	s.sanitizeWideAll()
}

func (s *Screen) eraseLine(mode int) {
	blank := Cell{Ch: ' ', Style: s.style}
	row := s.active.y * s.cols
	s.active.wrap = false
	switch mode {
	case 1:
		fillCells(s.active.cells[row:row+s.active.x+1], blank)
	case 2:
		fillCells(s.active.cells[row:row+s.cols], blank)
	default:
		fillCells(s.active.cells[row+s.active.x:row+s.cols], blank)
	}
	s.sanitizeWideRow(s.active.y)
}

func (s *Screen) eraseChars(n int) {
	if n <= 0 {
		n = 1
	}
	end := clamp(s.active.x+n, 0, s.cols)
	row := s.active.y * s.cols
	fillCells(s.active.cells[row+s.active.x:row+end], Cell{Ch: ' ', Style: s.style})
	s.active.wrap = false
	s.sanitizeWideRow(s.active.y)
}

func (s *Screen) insertChars(n int) {
	n = clamp(n, 1, s.cols-s.active.x)
	row := s.active.y * s.cols
	line := s.active.cells[row : row+s.cols]
	copy(line[s.active.x+n:], line[s.active.x:s.cols-n])
	fillCells(line[s.active.x:s.active.x+n], Cell{Ch: ' ', Style: s.style})
	s.active.wrap = false
	s.sanitizeWideRow(s.active.y)
}

func (s *Screen) deleteChars(n int) {
	n = clamp(n, 1, s.cols-s.active.x)
	row := s.active.y * s.cols
	line := s.active.cells[row : row+s.cols]
	copy(line[s.active.x:], line[s.active.x+n:])
	fillCells(line[s.cols-n:], Cell{Ch: ' ', Style: s.style})
	s.active.wrap = false
	s.sanitizeWideRow(s.active.y)
}

func (s *Screen) insertLines(n int) {
	if s.active.y < s.active.scrollTop || s.active.y > s.active.scrollBottom {
		return
	}
	n = clamp(n, 1, s.active.scrollBottom-s.active.y+1)
	s.scrollDown(s.active.y, s.active.scrollBottom, n)
}

func (s *Screen) deleteLines(n int) {
	if s.active.y < s.active.scrollTop || s.active.y > s.active.scrollBottom {
		return
	}
	n = clamp(n, 1, s.active.scrollBottom-s.active.y+1)
	s.scrollUp(s.active.y, s.active.scrollBottom, n)
}

func (s *Screen) scrollUp(top, bottom, n int) {
	if top < 0 || bottom >= s.rows || top > bottom {
		return
	}
	n = clamp(n, 1, bottom-top+1)
	for row := top; row <= bottom-n; row++ {
		copy(s.row(row), s.row(row+n))
	}
	blank := Cell{Ch: ' ', Style: s.style}
	for row := bottom - n + 1; row <= bottom; row++ {
		fillCells(s.row(row), blank)
	}
}

func (s *Screen) scrollDown(top, bottom, n int) {
	if top < 0 || bottom >= s.rows || top > bottom {
		return
	}
	n = clamp(n, 1, bottom-top+1)
	for row := bottom; row >= top+n; row-- {
		copy(s.row(row), s.row(row-n))
	}
	blank := Cell{Ch: ' ', Style: s.style}
	for row := top; row < top+n; row++ {
		fillCells(s.row(row), blank)
	}
}

func (s *Screen) row(row int) []Cell {
	start := row * s.cols
	return s.active.cells[start : start+s.cols]
}

func (s *Screen) setScrollRegion(params csiParams) {
	top := params.value(0, 1) - 1
	bottom := params.value(1, s.rows) - 1
	if top < 0 || bottom >= s.rows || top >= bottom {
		s.active.scrollTop = 0
		s.active.scrollBottom = s.rows - 1
	} else {
		s.active.scrollTop = top
		s.active.scrollBottom = bottom
	}
	s.moveCursorTo(0, 0)
}

func (s *Screen) applySGR(params csiParams) {
	if params.n == 0 {
		s.style = Style{}
		return
	}
	for i := 0; i < params.n; i++ {
		code := params.value(i, 0)
		switch {
		case code == 0:
			s.style = Style{}
		case code == 1:
			s.style.Bold = true
		case code == 2:
			s.style.Dim = true
		case code == 3:
			s.style.Italic = true
		case code == 4:
			s.style.Underline = true
		case code == 5 || code == 6:
			s.style.Blink = true
		case code == 7:
			s.style.Inverse = true
		case code == 8:
			s.style.Hidden = true
		case code == 9:
			s.style.Strikethrough = true
		case code == 21:
			s.style.Underline = true
		case code == 22:
			s.style.Bold = false
			s.style.Dim = false
		case code == 23:
			s.style.Italic = false
		case code == 24:
			s.style.Underline = false
		case code == 25:
			s.style.Blink = false
		case code == 27:
			s.style.Inverse = false
		case code == 28:
			s.style.Hidden = false
		case code == 29:
			s.style.Strikethrough = false
		case code == 39:
			s.style.Fg = Color{}
		case code == 49:
			s.style.Bg = Color{}
		case code == 53:
			s.style.Overline = true
		case code == 55:
			s.style.Overline = false
		case code >= 30 && code <= 37:
			s.style.Fg = Color{Mode: ColorIndexed, Index: uint8(code - 30)}
		case code >= 40 && code <= 47:
			s.style.Bg = Color{Mode: ColorIndexed, Index: uint8(code - 40)}
		case code >= 90 && code <= 97:
			s.style.Fg = Color{Mode: ColorIndexed, Index: uint8(code - 90 + 8)}
		case code >= 100 && code <= 107:
			s.style.Bg = Color{Mode: ColorIndexed, Index: uint8(code - 100 + 8)}
		case code == 38 || code == 48:
			color, consumed, ok := extendedColor(params, i+1)
			if ok {
				if code == 38 {
					s.style.Fg = color
				} else {
					s.style.Bg = color
				}
				i += consumed
			}
		}
	}
}

func extendedColor(params csiParams, start int) (Color, int, bool) {
	mode := params.value(start, -1)
	switch mode {
	case 5:
		idx := params.value(start+1, -1)
		if idx >= 0 && idx <= 255 {
			return Color{Mode: ColorIndexed, Index: uint8(idx)}, 2, true
		}
	case 2:
		r := params.value(start+1, -1)
		g := params.value(start+2, -1)
		b := params.value(start+3, -1)
		if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
			return Color{Mode: ColorRGB, R: uint8(r), G: uint8(g), B: uint8(b)}, 4, true
		}
	}
	return Color{}, 0, false
}

func (s *Screen) setModes(params csiParams, enabled bool) bool {
	if params.private != '?' {
		return false
	}
	dirty := false
	for i := 0; i < params.n; i++ {
		mode := params.value(i, 0)
		switch mode {
		case 1:
			// DECCKM: application cursor keys. Does not change the rendered
			// screen, but the pane uses it to translate arrow-key input.
			s.applicationCursor = enabled
		case 6:
			if s.originMode != enabled {
				s.originMode = enabled
				dirty = true
			}
			if s.moveCursorTo(0, 0) {
				dirty = true
			}
		case 7:
			if s.autoWrap != enabled {
				s.autoWrap = enabled
				s.active.wrap = false
				dirty = true
			}
		case 25:
			if s.cursorVisible != enabled {
				s.cursorVisible = enabled
				dirty = true
			}
		case 47, 1047:
			if s.setAlternate(enabled, true) {
				dirty = true
			}
		case 1048:
			if enabled {
				s.saveCursor()
			} else if s.restoreCursor() {
				dirty = true
			}
		case 1049:
			if enabled {
				s.saveCursor()
				if s.setAlternate(true, true) {
					dirty = true
				}
			} else {
				if s.setAlternate(false, false) {
					dirty = true
				}
				if s.restoreCursor() {
					dirty = true
				}
			}
		}
	}
	return dirty
}

func (s *Screen) setAlternate(enabled bool, clear bool) bool {
	if enabled {
		wasAlternate := s.active == &s.alternate
		s.active = &s.alternate
		if clear {
			fillCells(s.active.cells, BlankCell())
			s.active.x = 0
			s.active.y = 0
			s.active.wrap = false
			s.active.scrollTop = 0
			s.active.scrollBottom = s.rows - 1
		}
		return !wasAlternate || clear
	}
	if s.active == &s.normal {
		return false
	}
	s.active = &s.normal
	return true
}

func (s *Screen) reset() {
	s.normal = newBuffer(s.cols, s.rows)
	s.alternate = newBuffer(s.cols, s.rows)
	s.active = &s.normal
	s.style = Style{}
	s.cursorVisible = true
	s.autoWrap = true
	s.originMode = false
	s.applicationCursor = false
	s.lineDrawing = false
	s.parser = stateGround
	s.csiLen = 0
	s.utf8Len = 0
	s.lastCell = Cell{}
	s.lastWidth = 0
	s.hasLastCell = false
	s.tabStops = defaultTabStops(s.cols)
}

func (s *Screen) softReset() bool {
	dirty := false
	if !s.cursorVisible || s.active.wrap {
		dirty = true
	}
	s.style = Style{}
	s.cursorVisible = true
	s.autoWrap = true
	s.originMode = false
	s.applicationCursor = false
	s.lineDrawing = false
	s.active.wrap = false
	s.active.scrollTop = 0
	s.active.scrollBottom = s.rows - 1
	s.lastCell = Cell{}
	s.lastWidth = 0
	s.hasLastCell = false
	if s.moveCursor(0, 0) {
		dirty = true
	}
	return dirty
}

func (s *Screen) alignmentTest() {
	fillCells(s.active.cells, Cell{Ch: 'E', Style: s.style})
	s.moveCursor(0, 0)
}

type csiParams struct {
	private byte
	values  [32]int
	set     [32]bool
	n       int
}

func (p csiParams) value(i int, def int) int {
	if i < 0 || i >= p.n || !p.set[i] {
		return def
	}
	return p.values[i]
}

func parseParams(raw []byte) csiParams {
	var p csiParams
	idx := 0
	if len(raw) > 0 && (raw[0] == '?' || raw[0] == '>' || raw[0] == '!') {
		p.private = raw[0]
		idx = 1
	}
	value := 0
	hasValue := false
	for ; idx < len(raw); idx++ {
		b := raw[idx]
		if b >= '0' && b <= '9' {
			value = value*10 + int(b-'0')
			hasValue = true
			continue
		}
		if b == ';' || b == ':' {
			p.add(value, hasValue)
			value = 0
			hasValue = false
		}
	}
	if len(raw) > 0 || hasValue {
		p.add(value, hasValue)
	}
	return p
}

func (p *csiParams) add(value int, set bool) {
	if p.n >= len(p.values) {
		return
	}
	p.values[p.n] = value
	p.set[p.n] = set
	p.n++
}

func decodeBufferedRune(data []byte) (rune, int, bool) {
	if !utf8.FullRune(data) {
		return 0, len(data), false
	}
	r, size := utf8.DecodeRune(data)
	return r, size, true
}

func fillCells(cells []Cell, cell Cell) {
	for i := range cells {
		cells[i] = cell
	}
}

func (s *Screen) sanitizeWideAll() {
	for row := 0; row < s.rows; row++ {
		s.sanitizeWideRow(row)
	}
}

func (s *Screen) sanitizeWideRow(row int) {
	if row < 0 || row >= s.rows {
		return
	}
	cells := s.row(row)
	blank := Cell{Ch: ' ', Style: s.style}
	for col := 0; col < len(cells); col++ {
		cell := cells[col]
		if cell.WideContinuation {
			if col == 0 || !cells[col-1].Wide {
				cells[col] = blank
			}
			continue
		}
		if cell.Wide && (col+1 >= len(cells) || !cells[col+1].WideContinuation) {
			cell.Wide = false
			cells[col] = cell
		}
	}
}

func isCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

func runeWidth(r rune) int {
	if isCombining(r) {
		return 0
	}
	if r == 0 || r < ' ' || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		(r >= 0x2329 && r <= 0x232a) ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}

func defaultTabStops(cols int) []bool {
	stops := make([]bool, cols)
	for col := 8; col < cols; col += 8 {
		stops[col] = true
	}
	return stops
}

func (s *Screen) setTabStop() {
	if s.active.x >= 0 && s.active.x < len(s.tabStops) {
		s.tabStops[s.active.x] = true
	}
}

func (s *Screen) clearTab(mode int) {
	switch mode {
	case 0:
		if s.active.x >= 0 && s.active.x < len(s.tabStops) {
			s.tabStops[s.active.x] = false
		}
	case 3:
		for i := range s.tabStops {
			s.tabStops[i] = false
		}
	}
}

func (s *Screen) nextTab(x, n int) int {
	if n <= 0 {
		n = 1
	}
	pos := x
	for ; n > 0; n-- {
		found := false
		for col := pos + 1; col < s.cols; col++ {
			if col < len(s.tabStops) && s.tabStops[col] {
				pos = col
				found = true
				break
			}
		}
		if !found {
			return s.cols - 1
		}
	}
	return pos
}

func (s *Screen) previousTab(x, n int) int {
	if n <= 0 {
		n = 1
	}
	pos := x
	for ; n > 0; n-- {
		found := false
		for col := pos - 1; col >= 0; col-- {
			if col < len(s.tabStops) && s.tabStops[col] {
				pos = col
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}
	return pos
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func decLineRune(b byte) rune {
	switch b {
	case '`':
		return '\u25c6'
	case 'a':
		return '\u2592'
	case 'f':
		return '\u00b0'
	case 'g':
		return '\u00b1'
	case 'j':
		return '\u2518'
	case 'k':
		return '\u2510'
	case 'l':
		return '\u250c'
	case 'm':
		return '\u2514'
	case 'n':
		return '\u253c'
	case 'q':
		return '\u2500'
	case 't':
		return '\u251c'
	case 'u':
		return '\u2524'
	case 'v':
		return '\u2534'
	case 'w':
		return '\u252c'
	case 'x':
		return '\u2502'
	case 'y':
		return '\u2264'
	case 'z':
		return '\u2265'
	case '{':
		return '\u03c0'
	case '|':
		return '\u2260'
	case '}':
		return '\u00a3'
	case '~':
		return '\u00b7'
	default:
		return rune(b)
	}
}
