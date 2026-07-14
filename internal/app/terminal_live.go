package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	termemu "github.com/shellcell/ttysvg/internal/terminal"
	"golang.org/x/term"
)

type liveTerminal struct {
	mu         sync.Mutex
	inputMu    sync.Mutex
	mouseMu    sync.Mutex
	stdout     *os.File
	writer     io.Writer
	layout     paneLayout
	cfg        Config
	wantBg     bool
	decorated  bool
	alternate  bool
	background bool
	inputBuf   []byte
	mouseModes map[int]bool
	restored   bool
}

type paneLayout struct {
	termCols    int
	termRows    int
	contentLeft int
	contentTop  int
	innerCols   int
	innerRows   int
	padCols     int
	padRows     int
	fullFrame   bool
	rightCol    int
	statusRow   int
	sideCol     int
	sideWidth   int
}

func setupLiveTerminal(stdout *os.File, cfg Config) (*liveTerminal, error) {
	live := &liveTerminal{stdout: stdout, writer: stdout, cfg: cfg}
	if stdout == nil || !term.IsTerminal(int(stdout.Fd())) {
		return live, nil
	}
	if cfg.Headless {
		// Record the requested size directly, with no interactive pane, even on a
		// real terminal. Output still streams to stdout; the event log is captured
		// at cfg.Cols x cfg.Rows regardless of the visible terminal size.
		live.wantBg = cfg.Background != "" && cfg.Colors.Background != ""
		return live, nil
	}
	if !cfg.FixedSize {
		live.wantBg = cfg.Background != "" && cfg.Colors.Background != ""
		return live, nil
	}
	termCols, termRows, err := term.GetSize(int(stdout.Fd()))
	if err != nil {
		return nil, fmt.Errorf("determine current terminal size: %w", err)
	}
	if termCols <= 0 || termRows <= 0 {
		return nil, errors.New("determine current terminal size: invalid size")
	}
	if cfg.Cols > termCols || cfg.Rows > termRows {
		return nil, fmt.Errorf("recording size %dx%d is larger than current terminal %dx%d; resize the terminal first or choose a smaller size", cfg.Cols, cfg.Rows, termCols, termRows)
	}
	if cfg.Cols == termCols && cfg.Rows == termRows {
		live.wantBg = cfg.Background != "" && cfg.Colors.Background != ""
		return live, nil
	}

	padCols, padRows := livePaddingCells(cfg)
	innerCols := cfg.Cols + padCols*2
	innerRows := cfg.Rows + padRows*2
	if innerCols > termCols || innerRows > termRows {
		return nil, fmt.Errorf("recording size %dx%d with -padding %.2f needs at least %dx%d terminal cells; current terminal is %dx%d", cfg.Cols, cfg.Rows, cfg.Padding, innerCols, innerRows, termCols, termRows)
	}
	layout := newPaneLayout(cfg.Cols, cfg.Rows, termCols, termRows, padCols, padRows)
	if !layout.hasControls() {
		return nil, fmt.Errorf("recording size %dx%d fits in current terminal %dx%d but leaves no room for pane controls; resize the terminal first or choose a smaller size", cfg.Cols, cfg.Rows, termCols, termRows)
	}
	live.layout = layout
	live.decorated = true
	return live, nil
}

func (t *liveTerminal) Activate() {
	if t == nil || t.stdout == nil || !term.IsTerminal(int(t.stdout.Fd())) {
		return
	}
	if t.wantBg && t.cfg.Colors.Background != "" {
		applyTerminalBackground(t.stdout, t.cfg.Colors.Background)
		t.background = true
	}
	if t.decorated {
		_, _ = t.stdout.WriteString("\x1b[?1049h\x1b[H\x1b[2J\x1b[3J")
		enablePaneMouseModes(t.stdout)
		t.alternate = true
		drawRecordingFrame(t.stdout, t.cfg, t.layout, false)
		t.writer = newPaneWriter(t.stdout, t.cfg, t.layout, &t.mu, t)
	}
}

func newPaneLayout(cols, rows, termCols, termRows, padCols, padRows int) paneLayout {
	innerCols := cols + padCols*2
	innerRows := rows + padRows*2
	if termCols >= innerCols+2 && termRows >= innerRows+3 {
		return paneLayout{termCols: termCols, termRows: termRows, contentLeft: 2 + padCols, contentTop: 2 + padRows, innerCols: innerCols, innerRows: innerRows, padCols: padCols, padRows: padRows, fullFrame: true, rightCol: innerCols + 2, statusRow: innerRows + 3}
	}
	layout := paneLayout{termCols: termCols, termRows: termRows, contentLeft: 1 + padCols, contentTop: 1 + padRows, innerCols: innerCols, innerRows: innerRows, padCols: padCols, padRows: padRows}
	if termCols > innerCols {
		layout.rightCol = innerCols + 1
		layout.sideCol = innerCols + 2
		layout.sideWidth = termCols - innerCols - 1
	}
	if termRows > innerRows {
		layout.statusRow = innerRows + 1
	}
	return layout
}

func livePaddingCells(cfg Config) (int, int) {
	if cfg.Padding <= 0 {
		return 0, 0
	}
	cellWidth := cfg.CellWidth
	if cellWidth == 0 {
		cellWidth = cfg.FontSize * 0.62
	}
	cellHeight := cfg.CellHeight
	if cellHeight == 0 {
		cellHeight = cfg.FontSize * 1.25
	}
	if cellWidth <= 0 || cellHeight <= 0 {
		return 0, 0
	}
	return int(math.Ceil(cfg.Padding / cellWidth)), int(math.Ceil(cfg.Padding / cellHeight))
}

func (layout paneLayout) controlWidth(_ int) int {
	if layout.fullFrame {
		return layout.innerCols + 2
	}
	return layout.innerCols
}

func (layout paneLayout) hasControls() bool {
	const minControlWidth = 8
	return (layout.statusRow > 0 && layout.controlWidth(0) >= minControlWidth) || (layout.sideCol > 0 && layout.sideWidth >= minControlWidth && layout.innerRows >= 6)
}

func (t *liveTerminal) Writer() io.Writer {
	if t == nil || t.writer == nil {
		return io.Discard
	}
	return t.writer
}

func (t *liveTerminal) Decorated() bool {
	return t != nil && t.decorated
}

func (t *liveTerminal) Restore() {
	if t == nil || t.restored || t.stdout == nil || !term.IsTerminal(int(t.stdout.Fd())) {
		return
	}
	t.restored = true
	if releaser, ok := t.writer.(interface{ Release() }); ok {
		releaser.Release()
	}
	if t.background {
		restoreTerminalBackground(t.stdout)
	}
	if t.decorated {
		t.mu.Lock()
		defer t.mu.Unlock()
		disableMouseModes(t.stdout)
		if t.alternate {
			_, _ = t.stdout.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
		} else {
			_, _ = t.stdout.WriteString("\x1b[0m\x1b[?25h")
		}
	}
}

func (t *liveTerminal) RecordingPrefix() []byte {
	if !t.Decorated() {
		return nil
	}
	if prefixer, ok := t.writer.(interface{ RecordingPrefix() []byte }); ok {
		return prefixer.RecordingPrefix()
	}
	return nil
}

func (t *liveTerminal) SnapshotFrame() (termemu.Frame, bool, error) {
	if !t.Decorated() {
		return termemu.Frame{}, false, nil
	}
	if snapper, ok := t.writer.(interface{ SnapshotFrame() (termemu.Frame, error) }); ok {
		frame, err := snapper.SnapshotFrame()
		return frame, true, err
	}
	return termemu.Frame{}, false, nil
}

// interval is the upper bound on preview latency; a leading-edge repaint keeps
// the first byte of an idle period responsive.
const (
	paneFlushInterval      = 33 * time.Millisecond
	paneFlushIntervalLarge = 50 * time.Millisecond
	paneLargeCellThreshold = 20000
)

func paneFlushIntervalFor(cols, rows int) time.Duration {
	if cols*rows > paneLargeCellThreshold {
		return paneFlushIntervalLarge
	}
	return paneFlushInterval
}

type paneWriter struct {
	stdout      *os.File
	screen      *termemu.Screen
	prev        termemu.Frame
	pal         livePalette
	layout      paneLayout
	mu          *sync.Mutex
	live        *liveTerminal
	cols        int
	rows        int
	interval    time.Duration
	lastRender  time.Time
	timer       *time.Timer
	renderCount int
	styleIn     termemu.Style
	styleOut    liveStyle
	styleCached bool
	buf         bytes.Buffer // reused across repaints to avoid a per-render allocation
	appCursor   atomic.Bool  // mirrors the emulator's DECCKM for the input goroutine
	dirty       bool
	pending     bool
	closed      bool
}

// resolveStyle memoizes the most recent terminal-style → liveStyle mapping.
// Adjacent cells almost always share a style, so this collapses a run to a
// single palette resolution + hex parse. Cell-shape bits (wide/continuation)
// do not affect the rendered style and are masked so they cannot fragment the
// memoized entry.
func (p *paneWriter) resolveStyle(in termemu.Style) liveStyle {
	in = in.Visual()
	if p.styleCached && in == p.styleIn {
		return p.styleOut
	}
	out := p.pal.style(in)
	p.styleIn, p.styleOut, p.styleCached = in, out, true
	return out
}

// writeStyledCells emits a row of cells with minimal SGR transitions. Callers
// are responsible for writing the cursor-position prefix; this handles styling
// and the trailing reset only.
func (p *paneWriter) writeStyledCells(b *bytes.Buffer, cells []termemu.Cell) {
	var prev liveStyle
	prevSet := false
	for i := range cells {
		cell := cells[i]
		style := p.resolveStyle(cell.Style)
		if !prevSet || style != prev {
			style.appendSGR(b)
			prev = style
			prevSet = true
		}
		if cell.WideContinuation() {
			continue
		}
		writePaneCell(b, cell)
	}
	b.WriteString("\x1b[0m")
}

func newPaneWriter(stdout *os.File, cfg Config, layout paneLayout, mu *sync.Mutex, live *liveTerminal) *paneWriter {
	screen := newEmulatorScreen(cfg.Cols, cfg.Rows)
	p := &paneWriter{
		stdout:   stdout,
		screen:   screen,
		prev:     screen.Snapshot(),
		pal:      newLivePalette(cfg),
		layout:   layout,
		mu:       mu,
		live:     live,
		cols:     cfg.Cols,
		rows:     cfg.Rows,
		interval: paneFlushIntervalFor(cfg.Cols, cfg.Rows),
	}
	p.renderRows(p.prev)
	p.moveCursor(p.prev)
	return p
}

func (p *paneWriter) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return len(data), nil
	}

	if mouseModes := p.live.mouseModeSequences(data); len(mouseModes) > 0 {
		if _, err := p.stdout.Write(mouseModes); err != nil {
			return 0, err
		}
		enablePaneMouseModes(p.stdout)
	}
	if p.screen.Write(data) {
		p.dirty = true
	}
	// Publish the cursor-key mode so the input goroutine can translate arrows.
	p.appCursor.Store(p.screen.ApplicationCursorKeys())
	if err := p.scheduleRenderLocked(); err != nil {
		return 0, err
	}
	return len(data), nil
}

// applicationCursorKeys reports whether the child currently has DECCKM enabled.
func (t *liveTerminal) applicationCursorKeys() bool {
	if pw, ok := t.writer.(*paneWriter); ok {
		return pw.appCursor.Load()
	}
	return false
}

// scheduleRenderLocked coalesces repaints: it repaints immediately if at least
// one interval has elapsed since the last repaint (leading edge), otherwise it
// arms a single trailing timer so writes arriving inside the window collapse
// into one repaint. Must be called with p.mu held.
func (p *paneWriter) scheduleRenderLocked() error {
	if p.closed || !p.dirty || p.pending {
		return nil
	}
	if p.interval <= 0 {
		return p.renderNowLocked(false)
	}
	elapsed := time.Since(p.lastRender)
	if elapsed >= p.interval {
		return p.renderNowLocked(false)
	}
	p.pending = true
	p.timer = time.AfterFunc(p.interval-elapsed, p.flushPending)
	return nil
}

func (p *paneWriter) flushPending() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending = false
	_ = p.renderNowLocked(false)
}

func (p *paneWriter) renderNowLocked(force bool) error {
	if p.closed {
		return nil
	}
	if !force && !p.dirty {
		return nil
	}
	p.lastRender = time.Now()
	p.renderCount++
	next := p.screen.Snapshot()
	b := &p.buf
	b.Reset()
	for row := 0; row < p.rows; row++ {
		if !paneCellsEqual(p.prev.Row(row), next.Row(row)) {
			p.writeRow(b, row, next.Row(row))
		}
	}
	p.writeCursor(b, next)
	if b.Len() > 0 {
		if _, err := p.stdout.Write(b.Bytes()); err != nil {
			next.Release()
			return err
		}
	}
	p.prev.Release()
	p.prev = next
	p.dirty = false
	return nil
}

func (p *paneWriter) renderRows(frame termemu.Frame) {
	var b bytes.Buffer
	for row := 0; row < p.rows; row++ {
		p.writeRow(&b, row, frame.Row(row))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.stdout.Write(b.Bytes())
}

func (p *paneWriter) moveCursor(frame termemu.Frame) {
	var b bytes.Buffer
	p.writeCursor(&b, frame)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.stdout.Write(b.Bytes())
}

func (p *paneWriter) RecordingPrefix() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	_ = p.renderNowLocked(true)
	frame := p.screen.Snapshot()
	defer frame.Release()
	return p.frameToANSI(frame)
}

func (p *paneWriter) SnapshotFrame() (termemu.Frame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return termemu.Frame{}, errors.New("pane is closed")
	}
	if err := p.renderNowLocked(true); err != nil {
		return termemu.Frame{}, err
	}
	return p.screen.Snapshot(), nil
}

func (p *paneWriter) frameToANSI(frame termemu.Frame) []byte {
	var b bytes.Buffer
	b.WriteString("\x1b[0m\x1b[H\x1b[2J")
	if frame.CursorVisible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}
	for row := 0; row < frame.Rows; row++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", row+1)
		p.writeStyledCells(&b, frame.Row(row))
	}
	x := frame.CursorX
	y := frame.CursorY
	if x < 0 {
		x = 0
	}
	if x >= frame.Cols {
		x = frame.Cols - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= frame.Rows {
		y = frame.Rows - 1
	}
	fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[0m", y+1, x+1)
	return append([]byte(nil), b.Bytes()...)
}

func (p *paneWriter) writeRow(b *bytes.Buffer, row int, cells []termemu.Cell) {
	fmt.Fprintf(b, "\x1b[%d;%dH", row+p.layout.contentTop, p.layout.contentLeft)
	p.writeStyledCells(b, cells)
}

func (p *paneWriter) writeCursor(b *bytes.Buffer, frame termemu.Frame) {
	if !frame.CursorVisible {
		b.WriteString("\x1b[?25l")
		return
	}
	x := frame.CursorX
	y := frame.CursorY
	if x < 0 {
		x = 0
	}
	if x >= p.cols {
		x = p.cols - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= p.rows {
		y = p.rows - 1
	}
	fmt.Fprintf(b, "\x1b[?25h\x1b[%d;%dH", y+p.layout.contentTop, x+p.layout.contentLeft)
}

func writePaneCell(b *bytes.Buffer, cell termemu.Cell) {
	r := cell.Rune()
	if r < 0x20 || r == 0x7f {
		b.WriteByte(' ')
		return
	}
	b.WriteString(cell.Text())
}

func paneCellsEqual(a, b []termemu.Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *paneWriter) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
	}
	p.prev.Release()
}
