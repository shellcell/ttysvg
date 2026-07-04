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

// Attr is a bit in the packed style/cell flag set. Packing the former bool
// fields into one uint16 shrinks Cell from 48 to 32 bytes, which cuts the cost
// of every snapshot copy, frame compare, and grid fill by a third.
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrInverse
	AttrHidden
	AttrStrikethrough
	AttrOverline
	// AttrWide and AttrWideContinuation are cell-shape flags rather than SGR
	// attributes; they live in the same bit set so Cell needs no extra field.
	AttrWide
	AttrWideContinuation
)

// cellShapeAttrs are the bits that describe cell geometry, not visual style.
const cellShapeAttrs = AttrWide | AttrWideContinuation

type Style struct {
	Fg    Color
	Bg    Color
	Attrs Attr
}

func (s Style) Has(a Attr) bool {
	return s.Attrs&a != 0
}

// Visual returns the style with cell-shape bits cleared, for comparing or
// caching by rendered appearance only.
func (s Style) Visual() Style {
	s.Attrs &^= cellShapeAttrs
	return s
}

type Cell struct {
	Combining string
	Ch        rune
	Style     Style
}

func BlankCell() Cell {
	return Cell{Ch: ' '}
}

func (c Cell) Wide() bool {
	return c.Style.Has(AttrWide)
}

func (c Cell) WideContinuation() bool {
	return c.Style.Has(AttrWideContinuation)
}

func (c Cell) Rune() rune {
	if c.Ch == 0 || c.WideContinuation() {
		return ' '
	}
	return c.Ch
}

func (c Cell) Text() string {
	if c.WideContinuation() {
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
