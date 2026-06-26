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
	box           *[]Cell // pool wrapper backing Data, nil for caller-built frames
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
	if f == nil {
		return
	}
	if f.box != nil {
		*f.box = f.Data[:cap(f.Data)]
		releaseCells(f.box)
		f.box = nil
	}
	f.Data = nil
}

// cellPool stores *[]Cell wrappers rather than []Cell so that returning a buffer
// to the pool reuses the original wrapper instead of boxing the slice header on
// every Put (which would cost one allocation per Snapshot/Release cycle).
var cellPool = sync.Pool{New: func() any { s := make([]Cell, 0); return &s }}

func acquireCells(n int) (*[]Cell, []Cell) {
	box := cellPool.Get().(*[]Cell)
	cells := *box
	if cap(cells) >= n {
		cells = cells[:n]
	} else {
		cells = make([]Cell, n)
	}
	*box = cells
	return box, cells
}

func releaseCells(box *[]Cell) {
	if cap(*box) > 1<<20 {
		return // let GC reclaim oversized buffers instead of pinning them
	}
	cellPool.Put(box)
}
