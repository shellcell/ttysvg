package terminal

import "sync"

const (
	ColorDefault uint8 = iota
	ColorIndexed
	ColorRGB
)

type Color struct {
	Mode  uint8
	Index uint8
	R     uint8
	G     uint8
	B     uint8
}

type Style struct {
	Fg            Color
	Bg            Color
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Blink         bool
	Inverse       bool
	Hidden        bool
	Strikethrough bool
	Overline      bool
}

type Cell struct {
	Ch               rune
	Combining        string
	Wide             bool
	WideContinuation bool
	Style            Style
}

func BlankCell() Cell {
	return Cell{Ch: ' '}
}

func (c Cell) Rune() rune {
	if c.Ch == 0 || c.WideContinuation {
		return ' '
	}
	return c.Ch
}

func (c Cell) Text() string {
	if c.WideContinuation {
		return ""
	}
	return string(c.Rune()) + c.Combining
}

type Frame struct {
	Cols          int
	Rows          int
	Data          []Cell
	CursorX       int
	CursorY       int
	CursorVisible bool
}

func (f Frame) Row(row int) []Cell {
	start := row * f.Cols
	return f.Data[start : start+f.Cols]
}

func (f Frame) Equal(other Frame) bool {
	if f.Cols != other.Cols || f.Rows != other.Rows || len(f.Data) != len(other.Data) || f.CursorX != other.CursorX || f.CursorY != other.CursorY || f.CursorVisible != other.CursorVisible {
		return false
	}
	for i := range f.Data {
		if f.Data[i] != other.Data[i] {
			return false
		}
	}
	return true
}

func (f *Frame) Release() {
	if f == nil || f.Data == nil {
		return
	}
	releaseCells(f.Data)
	f.Data = nil
}

var cellPool sync.Pool

func acquireCells(n int) []Cell {
	if v := cellPool.Get(); v != nil {
		cells := v.([]Cell)
		if cap(cells) >= n {
			return cells[:n]
		}
	}
	return make([]Cell, n)
}

func releaseCells(cells []Cell) {
	if cap(cells) > 1<<20 {
		return
	}
	cellPool.Put(cells[:cap(cells)])
}
