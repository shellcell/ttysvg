package svg

import (
	"io"
	"testing"
	"time"

	"github.com/shellcell/ttysvg/internal/terminal"
)

// benchFrame builds a row of styled, mostly-text cells to exercise the per-cell
// emit path (text runs, background runs, escaping).
func benchFrame(cols, rows int, shift int) terminal.Frame {
	data := make([]terminal.Cell, cols*rows)
	text := []rune("the quick brown fox jumps over the lazy dog 0123456789")
	for i := range data {
		r := text[(i+shift)%len(text)]
		// Colors change in runs of 8 cells, as typical terminal output does.
		style := terminal.Style{Fg: terminal.Color{Mode: terminal.ColorIndexed, Index: uint8(((i + shift) / 8) % 8)}}
		data[i] = terminal.Cell{Ch: r, Style: style}
	}
	return terminal.Frame{Cols: cols, Rows: rows, Data: data}
}

// Phase: svg rendering.
// The final pass that turns recorded frames into animated SVG. Two frames are
// alternated so every row is re-emitted each iteration.
func BenchmarkSVGRender(b *testing.B) {
	const cols, rows = 120, 40
	frames := [2]terminal.Frame{benchFrame(cols, rows, 0), benchFrame(cols, rows, 1)}
	r := NewRenderer(io.Discard, Config{Cols: cols, Rows: rows, Theme: "dark", FontSize: 14})
	if err := r.Begin(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.WriteFrame(frames[i&1], time.Duration(i)*time.Millisecond, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}
}
