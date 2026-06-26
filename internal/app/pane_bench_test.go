package app

import (
	"os"
	"sync"
	"testing"
	"time"
)

func newTestPaneWriter(t testing.TB, cols, rows int) (*paneWriter, func()) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	cfg := Config{Cols: cols, Rows: rows, Theme: "dark", FontSize: 14}
	layout := newPaneLayout(cols, rows, cols+4, rows+4, 0, 0)
	mu := &sync.Mutex{}
	live := &liveTerminal{stdout: devnull, cfg: cfg}
	p := newPaneWriter(devnull, cfg, layout, mu, live)
	return p, func() {
		p.Release()
		_ = devnull.Close()
	}
}

// TestPaneWriterCoalescesRenders verifies the core of the optimization: a burst
// of PTY reads must not trigger one full snapshot+diff+repaint per read. With
// leading-edge + trailing coalescing, a tight burst collapses to a single
// repaint rather than one per write.
func TestPaneWriterCoalescesRenders(t *testing.T) {
	p, cleanup := newTestPaneWriter(t, 80, 24)
	defer cleanup()

	const writes = 500
	start := p.renderCount
	for i := 0; i < writes; i++ {
		if _, err := p.Write([]byte("x")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	renders := p.renderCount - start
	if renders > 5 {
		t.Fatalf("expected burst of %d writes to coalesce into a handful of repaints, got %d", writes, renders)
	}
	if renders == 0 {
		t.Fatal("expected at least one leading-edge repaint")
	}
}

// TestPaneWriterTrailingRender verifies the final screen state is still painted
// after a burst settles, so the preview never lags behind the child output.
func TestPaneWriterTrailingRender(t *testing.T) {
	p, cleanup := newTestPaneWriter(t, 80, 24)
	defer cleanup()

	if _, err := p.Write([]byte("a")); err != nil { // leading-edge repaint
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("b")); err != nil { // arms trailing timer
		t.Fatal(err)
	}
	p.mu.Lock()
	pending := p.pending
	p.mu.Unlock()
	if !pending {
		t.Fatal("expected a trailing repaint to be scheduled")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		done := !p.pending
		p.mu.Unlock()
		if done {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("trailing repaint never fired")
}

func TestPaneFlushIntervalFor(t *testing.T) {
	if got := paneFlushIntervalFor(80, 24); got != paneFlushInterval {
		t.Fatalf("small pane interval = %v, want %v", got, paneFlushInterval)
	}
	if got := paneFlushIntervalFor(300, 100); got != paneFlushIntervalLarge {
		t.Fatalf("large pane interval = %v, want %v", got, paneFlushIntervalLarge)
	}
}

// BenchmarkPaneWriterBurst measures the cost of feeding a burst of output
// through the pane writer (the live preview hot path).
func BenchmarkPaneWriterBurst(b *testing.B) {
	p, cleanup := newTestPaneWriter(b, 120, 40)
	defer cleanup()
	chunk := []byte("\x1b[32mok\x1b[0m  building target \x1b[2m(cached)\x1b[0m\r\n")
	b.ReportAllocs()
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}
