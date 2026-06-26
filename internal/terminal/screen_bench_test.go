package terminal

import (
	"runtime"
	"testing"
	"unsafe"
)

// sampleOutput is a representative burst of terminal output: colored text,
// cursor moves, and a scroll, similar to what a build tool or shell emits.
var sampleOutput = []byte("\x1b[32mok\x1b[0m  package/one\r\n" +
	"\x1b[31mFAIL\x1b[0m package/two\r\n" +
	"\x1b[1;34m=> building\x1b[0m widget \x1b[2m(cached)\x1b[0m\r\n" +
	"plain line of output that fills part of the row\r\n")

// Shared primitive (used by record-with-pane and svg rendering): parsing child
// output into the emulator's screen buffer.
func BenchmarkEmulatorScreenWrite(b *testing.B) {
	s := NewScreen(120, 40)
	b.ReportAllocs()
	b.SetBytes(int64(len(sampleOutput)))
	for i := 0; i < b.N; i++ {
		s.Write(sampleOutput)
	}
}

// Shared primitive (used by record-with-pane and svg rendering): copying the
// screen into an immutable frame for diffing.
func BenchmarkEmulatorSnapshot(b *testing.B) {
	s := NewScreen(120, 40)
	s.Write(sampleOutput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := s.Snapshot()
		frame.Release()
	}
}

// TestSnapshotMemoryIsPooled guards the cell pool that keeps Snapshot from
// allocating a fresh cols*rows buffer on every frame. The measured bytes per
// snapshot must stay far below one buffer's worth of cells, independent of the
// screen size — if the pool is removed this jumps to hundreds of KB per op.
func TestSnapshotMemoryIsPooled(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation accounting is unreliable under the race detector")
	}
	for _, dim := range []struct {
		cols, rows int
	}{{80, 24}, {200, 60}} {
		s := NewScreen(dim.cols, dim.rows)
		s.Write(sampleOutput)
		// Warm the pool so steady-state allocation is what we measure.
		for i := 0; i < 50; i++ {
			frame := s.Snapshot()
			frame.Release()
		}
		bytesPerOp := allocBytesPerOp(2000, func() {
			frame := s.Snapshot()
			frame.Release()
		})
		cellBuffer := uint64(dim.cols*dim.rows) * uint64(unsafe.Sizeof(Cell{}))
		if bytesPerOp > cellBuffer/8 {
			t.Fatalf("Snapshot(%dx%d) allocated %d B/op; expected pooled reuse well under %d B/op",
				dim.cols, dim.rows, bytesPerOp, cellBuffer/8)
		}
	}
}

// allocBytesPerOp returns the average heap bytes allocated per call of f.
// TotalAlloc is cumulative and unaffected by GC, so a generous threshold makes
// this a stable regression guard rather than a flaky micro-measurement.
func allocBytesPerOp(n int, f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < n; i++ {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(n)
}
