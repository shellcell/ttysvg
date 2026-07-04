package app

import (
	"os"
	"strconv"
	"strings"
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

// TestPaneInputTranslatesArrowsInApplicationMode covers the htop/ncdu arrow-key
// fix: when the child enables DECCKM, the pane must rewrite the terminal's
// ESC [ x cursor keys to the ESC O x form the child expects.
func TestPaneInputTranslatesArrowsInApplicationMode(t *testing.T) {
	p, cleanup := newTestPaneWriter(t, 80, 24)
	defer cleanup()
	live := p.live
	live.decorated = true
	live.writer = p

	if got := live.FilterInput([]byte("\x1b[A"), nil); string(got) != "\x1b[A" {
		t.Fatalf("normal mode should pass arrows unchanged, got %q", got)
	}

	if _, err := p.Write([]byte("\x1b[?1h")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want string }{
		{"\x1b[A", "\x1bOA"}, {"\x1b[B", "\x1bOB"},
		{"\x1b[C", "\x1bOC"}, {"\x1b[D", "\x1bOD"},
		{"\x1b[H", "\x1bOH"}, {"\x1b[F", "\x1bOF"},
	} {
		if got := live.FilterInput([]byte(tc.in), nil); string(got) != tc.want {
			t.Fatalf("app mode %q -> %q, want %q", tc.in, got, tc.want)
		}
	}

	// Modifier-encoded arrows keep their parameters and must not be rewritten.
	if got := live.FilterInput([]byte("\x1b[1;2A"), nil); string(got) != "\x1b[1;2A" {
		t.Fatalf("modified arrow should pass unchanged, got %q", got)
	}

	if _, err := p.Write([]byte("\x1b[?1l")); err != nil {
		t.Fatal(err)
	}
	if got := live.FilterInput([]byte("\x1b[A"), nil); string(got) != "\x1b[A" {
		t.Fatalf("after disabling app mode arrows should pass unchanged, got %q", got)
	}
}

func TestWriteUint8(t *testing.T) {
	for v := 0; v <= 255; v++ {
		var b strings.Builder
		writeUint8(&b, uint8(v))
		if got, want := b.String(), strconv.Itoa(v); got != want {
			t.Fatalf("writeUint8(%d) = %q, want %q", v, got, want)
		}
	}
}

func TestLiveStyleSGRTruecolor(t *testing.T) {
	s := liveStyle{fg: liveColor{r: 1, g: 22, b: 255, set: true}, bg: liveColor{r: 0, g: 100, b: 9, set: true}, bold: true}
	if got, want := s.sgr(), "\x1b[0;1;38;2;1;22;255;48;2;0;100;9m"; got != want {
		t.Fatalf("sgr() = %q, want %q", got, want)
	}
}

func TestPaneFlushIntervalFor(t *testing.T) {
	if got := paneFlushIntervalFor(80, 24); got != paneFlushInterval {
		t.Fatalf("small pane interval = %v, want %v", got, paneFlushInterval)
	}
	if got := paneFlushIntervalFor(300, 100); got != paneFlushIntervalLarge {
		t.Fatalf("large pane interval = %v, want %v", got, paneFlushIntervalLarge)
	}
}

func TestToggleKeyStartsAfterDoublePressWindow(t *testing.T) {
	control := newRecordingControl(noopEventSink{}, nil, nil)
	defer control.Close()

	control.ToggleKey()
	if control.Started() {
		t.Fatal("single Ctrl-\\ should wait for the double-press window before starting")
	}
	waitForControlState(t, control, recordingActive)
}

func TestToggleKeyDoublePressDoesNotStart(t *testing.T) {
	control := newRecordingControl(noopEventSink{}, nil, nil)
	defer control.Close()

	control.ToggleKey()
	control.ToggleKey()
	time.Sleep(keyDoublePressWindow + 20*time.Millisecond)
	if control.Started() {
		t.Fatal("double Ctrl-\\ should snapshot instead of starting")
	}
}

func TestToggleKeyPausesActiveAfterDoublePressWindow(t *testing.T) {
	control := newRecordingControl(noopEventSink{}, nil, nil)
	defer control.Close()
	control.StartOrResume()

	control.ToggleKey()
	if got := control.State(); got != recordingActive {
		t.Fatalf("single Ctrl-\\ should wait before pausing, state = %v", got)
	}
	waitForControlState(t, control, recordingPaused)
}

func TestToggleKeyDoublePressKeepsActive(t *testing.T) {
	control := newRecordingControl(noopEventSink{}, nil, nil)
	defer control.Close()
	control.StartOrResume()

	control.ToggleKey()
	control.ToggleKey()
	time.Sleep(keyDoublePressWindow + 20*time.Millisecond)
	if got := control.State(); got != recordingActive {
		t.Fatalf("double Ctrl-\\ should snapshot and keep recording, state = %v", got)
	}
}

func TestSnapshotMessageHold(t *testing.T) {
	if snapshotMessageHold != 2*time.Second {
		t.Fatalf("snapshotMessageHold = %v, want 2s", snapshotMessageHold)
	}
}

type noopEventSink struct{}

func (noopEventSink) WriteOutput(time.Duration, []byte) error { return nil }

func waitForControlState(t *testing.T, control *recordingControl, want recordingState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if control.State() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state = %v, want %v", control.State(), want)
}
