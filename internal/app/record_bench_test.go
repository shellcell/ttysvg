package app

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/rabarbra/ttysvg/internal/eventlog"
)

// recordChunk is a representative burst of child output (styled text + newline).
var recordChunk = []byte("\x1b[32mok\x1b[0m  building target \x1b[2m(cached)\x1b[0m\r\n")

// Phase: pure record (direct mode).
// Each PTY read is only appended to the event log; there is no terminal
// emulation or on-screen rendering. This is the floor cost of recording.
func BenchmarkRecordDirect(b *testing.B) {
	w := eventlog.NewWriter(io.Discard)
	b.ReportAllocs()
	b.SetBytes(int64(len(recordChunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.WriteOutput(time.Duration(i)*time.Millisecond, recordChunk); err != nil {
			b.Fatal(err)
		}
	}
}

// Phase: record with pane.
// In the decorated live-pane mode every PTY read additionally feeds the
// terminal emulator and repaints the pane, on top of the event-log write.
func BenchmarkRecordWithPane(b *testing.B) {
	p, cleanup := newTestPaneWriter(b, 120, 40)
	defer cleanup()
	w := eventlog.NewWriter(io.Discard)
	b.ReportAllocs()
	b.SetBytes(int64(len(recordChunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Write(recordChunk); err != nil {
			b.Fatal(err)
		}
		if err := w.WriteOutput(time.Duration(i)*time.Millisecond, recordChunk); err != nil {
			b.Fatal(err)
		}
	}
}

// Phase: pane rendering.
// The repaint itself: snapshot the emulator, diff against the previous frame
// and emit the changed rows as ANSI. Two full-screen fills are alternated so
// every row changes each iteration (worst case).
func BenchmarkPaneRender(b *testing.B) {
	const cols, rows = 120, 40
	p, cleanup := newTestPaneWriter(b, cols, rows)
	defer cleanup()
	// Box-drawing glyphs, as drawn by TUIs like htop/ncdu, so the per-cell text
	// emission exercises non-ASCII runes (one UTF-8 string per cell).
	fill := func(s string) []byte {
		return append([]byte("\x1b[H"), bytes.Repeat([]byte(s), cols*rows)...)
	}
	frames := [2][]byte{fill("│"), fill("─")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.screen.Write(frames[i&1])
		p.mu.Lock()
		p.dirty = true
		err := p.renderNowLocked(true)
		p.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}
