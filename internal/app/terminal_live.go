package app

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rabarbra/ttysvg/internal/svg"
	termemu "github.com/rabarbra/ttysvg/internal/terminal"
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

func (t *liveTerminal) FilterInput(data []byte, control *recordingControl) []byte {
	if !t.Decorated() || len(data) == 0 {
		return data
	}
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	return t.filterPaneInput(data, control)
}

func (t *liveTerminal) filterPaneInput(data []byte, control *recordingControl) []byte {
	buf := append(t.inputBuf, data...)
	t.inputBuf = nil
	out := make([]byte, 0, len(buf))
	for i := 0; i < len(buf); {
		if buf[i] != 0x1b || i+1 >= len(buf) || buf[i+1] != '[' {
			if !handleControlKey(buf[i], control) {
				out = append(out, buf[i])
			}
			i++
			continue
		}
		if i+2 >= len(buf) {
			t.inputBuf = append(t.inputBuf, buf[i:]...)
			break
		}
		if buf[i+2] == '<' {
			end := i + 3
			for end < len(buf) && buf[end] != 'M' && buf[end] != 'm' {
				end++
			}
			if end >= len(buf) {
				t.inputBuf = append(t.inputBuf, buf[i:]...)
				break
			}
			seq := buf[i : end+1]
			if !t.handleMouseControl(seq, control) && t.childMouseEnabled() {
				if translated, ok := t.translateSGRMouse(seq); ok {
					out = append(out, translated...)
				}
			}
			i = end + 1
			continue
		}
		if buf[i+2] == 'M' {
			if i+6 > len(buf) {
				t.inputBuf = append(t.inputBuf, buf[i:]...)
				break
			}
			seq := buf[i : i+6]
			if !t.handleMouseControl(seq, control) && t.childMouseEnabled() {
				if translated, ok := t.translateX10Mouse(seq); ok {
					out = append(out, translated...)
				}
			}
			i += 6
			continue
		}
		out = append(out, buf[i])
		i++
	}
	return out
}

func handleControlKey(b byte, control *recordingControl) bool {
	if control == nil {
		return false
	}
	switch b {
	case keyStart:
		control.StartOrResume()
		return true
	case keyPause:
		control.PauseOrResume()
		return true
	case keyStop:
		control.Stop()
		return true
	default:
		return false
	}
}

func (t *liveTerminal) handleMouseControl(seq []byte, control *recordingControl) bool {
	if control == nil {
		return false
	}
	x, y, press, ok := mousePoint(seq)
	if !ok || !press {
		return false
	}
	switch t.controlAt(x, y, control.State()) {
	case paneActionStart:
		control.StartOrResume()
		return true
	case paneActionPause:
		control.PauseOrResume()
		return true
	case paneActionStop:
		control.Stop()
		return true
	default:
		return false
	}
}

type paneAction uint8

const (
	paneActionNone paneAction = iota
	paneActionStart
	paneActionPause
	paneActionStop
)

func (t *liveTerminal) controlAt(x, y int, state recordingState) paneAction {
	buttons := controlButtons(state)
	if t.layout.statusRow > 0 && y == t.layout.statusRow {
		col := 2
		for _, button := range buttons {
			width := len(button.label) + 2
			if x >= col && x < col+width {
				return button.action
			}
			col += width + 1
		}
	}
	if t.layout.sideCol > 0 && x >= t.layout.sideCol && x < t.layout.sideCol+t.layout.sideWidth {
		for idx, button := range buttons {
			row := 4 + idx*2
			if y == row {
				return button.action
			}
		}
	}
	return paneActionNone
}

func mousePoint(seq []byte) (int, int, bool, bool) {
	if len(seq) >= 6 && seq[0] == 0x1b && seq[1] == '[' && seq[2] == 'M' {
		return int(seq[4]) - 32, int(seq[5]) - 32, true, true
	}
	if len(seq) < 6 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '<' {
		return 0, 0, false, false
	}
	body := string(seq[3 : len(seq)-1])
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return 0, 0, false, false
	}
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	if errX != nil || errY != nil {
		return 0, 0, false, false
	}
	return x, y, seq[len(seq)-1] == 'M', true
}

func (t *liveTerminal) childMouseEnabled() bool {
	t.mouseMu.Lock()
	defer t.mouseMu.Unlock()
	for mode, enabled := range t.mouseModes {
		if enabled && isMouseTrackingMode(mode) {
			return true
		}
	}
	return false
}

func (t *liveTerminal) translateSGRMouse(seq []byte) ([]byte, bool) {
	body := string(seq[3 : len(seq)-1])
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return seq, true
	}
	button, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return seq, true
	}
	x, y, ok := t.translateMousePoint(x, y)
	if !ok {
		return nil, false
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x, y, seq[len(seq)-1])), true
}

func (t *liveTerminal) translateX10Mouse(seq []byte) ([]byte, bool) {
	x := int(seq[4]) - 32
	y := int(seq[5]) - 32
	x, y, ok := t.translateMousePoint(x, y)
	if !ok || x < 1 || x > 223 || y < 1 || y > 223 {
		return nil, false
	}
	out := append([]byte(nil), seq...)
	out[4] = byte(x + 32)
	out[5] = byte(y + 32)
	return out, true
}

func (t *liveTerminal) translateMousePoint(x, y int) (int, int, bool) {
	x = x - t.layout.contentLeft + 1
	y = y - t.layout.contentTop + 1
	if x < 1 || x > t.cfg.Cols || y < 1 || y > t.cfg.Rows {
		return 0, 0, false
	}
	return x, y, true
}

func (t *liveTerminal) UpdateRecordingState(state recordingState, elapsed time.Duration) {
	if !t.Decorated() || t.stdout == nil || !term.IsTerminal(int(t.stdout.Fd())) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	drawLiveControls(t.stdout, t.cfg, t.layout, state, elapsed)
}

func drawRecordingFrame(stdout *os.File, cfg Config, layout paneLayout, recording bool) {
	pal := newLivePalette(cfg)
	border := pal.borderStyle().sgr()
	background := pal.backgroundStyle().sgr()
	reset := "\x1b[0m"
	var b strings.Builder
	b.WriteString(reset)
	fillPaneBackground(&b, layout, background, reset)
	if layout.fullFrame {
		frameWidth := layout.innerCols + 2
		writeAt(&b, 1, 1, border+borderLine(frameWidth, fmt.Sprintf(" ttysvg %dx%d ", cfg.Cols, cfg.Rows))+reset)
		for row := 0; row < layout.innerRows; row++ {
			writeAt(&b, row+2, 1, border+"|"+reset)
			writeAt(&b, row+2, layout.rightCol, border+"|"+reset)
		}
		writeAt(&b, layout.innerRows+2, 1, border+plainBorderLine(frameWidth)+reset)
	} else {
		if layout.rightCol > 0 {
			for row := 0; row < layout.innerRows; row++ {
				writeAt(&b, row+1, layout.rightCol, border+"|"+reset)
			}
		}
	}
	_, _ = stdout.WriteString(b.String())
	state := recordingPreparing
	if recording {
		state = recordingActive
	}
	drawLiveControls(stdout, cfg, layout, state, 0)
}

func drawLiveControls(stdout *os.File, cfg Config, layout paneLayout, state recordingState, elapsed time.Duration) {
	pal := newLivePalette(cfg)
	var b strings.Builder
	status := pal.statusStyle().sgr()
	dim := pal.dimStyle().sgr()
	reset := "\x1b[0m"
	message := controlMessage(cfg, state, elapsed)
	if layout.statusRow > 0 {
		writeAt(&b, layout.statusRow, 1, status+fitText(message, layout.controlWidth(cfg.Cols))+reset)
	} else if layout.sideCol > 0 && layout.sideWidth > 0 {
		lines := sideControlLines(cfg, state, elapsed)
		for row := 0; row < layout.innerRows; row++ {
			text := ""
			if row < len(lines) {
				text = lines[row]
			}
			writeAt(&b, row+1, layout.sideCol, dim+fitText(text, layout.sideWidth)+reset)
		}
	}
	_, _ = stdout.WriteString(b.String())
}

func fillPaneBackground(b *strings.Builder, layout paneLayout, style, reset string) {
	if layout.innerCols <= 0 || layout.innerRows <= 0 {
		return
	}
	rowOffset := 1
	colOffset := 1
	if layout.fullFrame {
		rowOffset = 2
		colOffset = 2
	}
	line := style + strings.Repeat(" ", layout.innerCols) + reset
	for row := 0; row < layout.innerRows; row++ {
		writeAt(b, row+rowOffset, colOffset, line)
	}
}

func formatRecordClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	hours := total / 3600
	minutes := (total / 60) % 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

type controlButton struct {
	label  string
	action paneAction
}

func controlButtons(state recordingState) []controlButton {
	switch state {
	case recordingActive:
		return []controlButton{{label: "Pause", action: paneActionPause}, {label: "Stop", action: paneActionStop}}
	case recordingPaused:
		return []controlButton{{label: "Resume", action: paneActionPause}, {label: "Stop", action: paneActionStop}}
	default:
		return []controlButton{{label: "Start", action: paneActionStart}, {label: "Stop", action: paneActionStop}}
	}
}

func controlMessage(cfg Config, state recordingState, elapsed time.Duration) string {
	var b strings.Builder
	b.WriteByte(' ')
	for _, button := range controlButtons(state) {
		fmt.Fprintf(&b, "[%s] ", button.label)
	}
	switch state {
	case recordingActive:
		fmt.Fprintf(&b, "REC %s ", formatRecordClock(elapsed))
	case recordingPaused:
		fmt.Fprintf(&b, "PAUSED %s ", formatRecordClock(elapsed))
	case recordingStopped:
		fmt.Fprintf(&b, "STOPPING %s ", formatRecordClock(elapsed))
	default:
		fmt.Fprintf(&b, "PREP %dx%d | Ctrl-R start Ctrl-P pause Ctrl-Q stop ", cfg.Cols, cfg.Rows)
	}
	return b.String()
}

func sideControlLines(cfg Config, state recordingState, elapsed time.Duration) []string {
	lines := []string{"ttysvg", fmt.Sprintf("%dx%d", cfg.Cols, cfg.Rows), ""}
	for _, button := range controlButtons(state) {
		lines = append(lines, button.label, "")
	}
	switch state {
	case recordingActive:
		lines = append(lines, "REC", formatRecordClock(elapsed))
	case recordingPaused:
		lines = append(lines, "PAUSED", formatRecordClock(elapsed))
	case recordingStopped:
		lines = append(lines, "STOP")
	default:
		lines = append(lines, "PREP", "Ctrl-R")
	}
	return lines
}

func writeAt(b *strings.Builder, row, col int, text string) {
	fmt.Fprintf(b, "\x1b[%d;%dH%s", row, col, text)
}

func borderLine(width int, title string) string {
	inner := width - 2
	if len(title)+1 < inner {
		return "+-" + title + strings.Repeat("-", inner-len(title)-1) + "+"
	}
	return plainBorderLine(width)
}

func plainBorderLine(width int) string {
	if width <= 1 {
		return strings.Repeat("-", max(width, 0))
	}
	return "+" + strings.Repeat("-", width-2) + "+"
}

func fitText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(text) > width {
		return text[:width]
	}
	return text + strings.Repeat(" ", width-len(text))
}

type paneWriter struct {
	stdout *os.File
	screen *termemu.Screen
	prev   termemu.Frame
	pal    livePalette
	layout paneLayout
	mu     *sync.Mutex
	live   *liveTerminal
	cols   int
	rows   int
	dirty  bool
	closed bool
}

func newPaneWriter(stdout *os.File, cfg Config, layout paneLayout, mu *sync.Mutex, live *liveTerminal) *paneWriter {
	screen := termemu.NewScreen(cfg.Cols, cfg.Rows)
	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}
	if info, ok := termemu.LoadTerminfo(termName); ok {
		screen.SetTerminfo(info)
	}
	p := &paneWriter{
		stdout: stdout,
		screen: screen,
		prev:   screen.Snapshot(),
		pal:    newLivePalette(cfg),
		layout: layout,
		mu:     mu,
		live:   live,
		cols:   cfg.Cols,
		rows:   cfg.Rows,
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
	if err := p.renderNowLocked(false); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (p *paneWriter) renderNowLocked(force bool) error {
	if p.closed {
		return nil
	}
	if !force && !p.dirty {
		return nil
	}
	next := p.screen.Snapshot()
	var b strings.Builder
	for row := 0; row < p.rows; row++ {
		if !paneCellsEqual(p.prev.Row(row), next.Row(row)) {
			p.writeRow(&b, row, next.Row(row))
		}
	}
	p.writeCursor(&b, next)
	if b.Len() > 0 {
		if _, err := p.stdout.WriteString(b.String()); err != nil {
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
	var b strings.Builder
	for row := 0; row < p.rows; row++ {
		p.writeRow(&b, row, frame.Row(row))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.stdout.WriteString(b.String())
}

func (p *paneWriter) moveCursor(frame termemu.Frame) {
	var b strings.Builder
	p.writeCursor(&b, frame)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.stdout.WriteString(b.String())
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

func (p *paneWriter) frameToANSI(frame termemu.Frame) []byte {
	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[H\x1b[2J")
	if frame.CursorVisible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}
	for row := 0; row < frame.Rows; row++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", row+1)
		var prev liveStyle
		prevSet := false
		for _, cell := range frame.Row(row) {
			style := p.pal.style(cell.Style)
			if !prevSet || style != prev {
				b.WriteString(style.sgr())
				prev = style
				prevSet = true
			}
			if cell.WideContinuation {
				continue
			}
			writePaneCell(&b, cell)
		}
		b.WriteString("\x1b[0m")
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
	return []byte(b.String())
}

func (p *paneWriter) writeRow(b *strings.Builder, row int, cells []termemu.Cell) {
	fmt.Fprintf(b, "\x1b[%d;%dH", row+p.layout.contentTop, p.layout.contentLeft)
	var prev liveStyle
	prevSet := false
	for _, cell := range cells {
		style := p.pal.style(cell.Style)
		if !prevSet || style != prev {
			b.WriteString(style.sgr())
			prev = style
			prevSet = true
		}
		if cell.WideContinuation {
			continue
		}
		writePaneCell(b, cell)
	}
	b.WriteString("\x1b[0m")
}

func (p *paneWriter) writeCursor(b *strings.Builder, frame termemu.Frame) {
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

func writePaneCell(b *strings.Builder, cell termemu.Cell) {
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

func (t *liveTerminal) mouseModeSequences(data []byte) []byte {
	t.mouseMu.Lock()
	defer t.mouseMu.Unlock()
	var out strings.Builder
	for i := 0; i < len(data); i++ {
		if data[i] != 0x1b || i+3 >= len(data) || data[i+1] != '[' || data[i+2] != '?' {
			continue
		}
		end := i + 3
		for end < len(data) && data[end] != 'h' && data[end] != 'l' {
			end++
		}
		if end >= len(data) {
			break
		}
		params := t.mouseModeParams(string(data[i+3:end]), data[end] == 'h')
		if len(params) > 0 {
			fmt.Fprintf(&out, "\x1b[?%s%c", strings.Join(params, ";"), data[end])
		}
		i = end
	}
	return []byte(out.String())
}

func (t *liveTerminal) mouseModeParams(raw string, enabled bool) []string {
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	if t.mouseModes == nil {
		t.mouseModes = map[int]bool{}
	}
	for _, part := range parts {
		mode, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if isMouseMode(mode) {
			t.mouseModes[mode] = enabled
			out = append(out, strconv.Itoa(mode))
		}
	}
	return out
}

func isMouseTrackingMode(mode int) bool {
	switch mode {
	case 9, 1000, 1002, 1003:
		return true
	default:
		return false
	}
}

func enablePaneMouseModes(stdout *os.File) {
	_, _ = stdout.WriteString("\x1b[?1000;1006h")
}

func isMouseMode(mode int) bool {
	switch mode {
	case 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 1016:
		return true
	default:
		return false
	}
}

func disableMouseModes(stdout *os.File) {
	_, _ = stdout.WriteString("\x1b[?9;1000;1002;1003;1004;1005;1006;1015;1016l")
}

func (p *paneWriter) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.prev.Release()
}

type livePalette struct {
	palette svg.Palette
}

func newLivePalette(cfg Config) livePalette {
	return livePalette{palette: svg.NewPalette(cfg.Theme, cfg.Colors)}
}

func (p livePalette) borderStyle() liveStyle {
	return liveStyle{fg: p.palette.Indexed(8), bg: p.palette.Background()}
}

func (p livePalette) backgroundStyle() liveStyle {
	return liveStyle{fg: p.palette.Foreground(), bg: p.palette.Background()}
}

func (p livePalette) statusStyle() liveStyle {
	return liveStyle{fg: p.palette.Background(), bg: p.palette.Foreground(), bold: true}
}

func (p livePalette) dimStyle() liveStyle {
	return liveStyle{fg: p.palette.Indexed(8), bg: p.palette.Background(), dim: true}
}

func (p livePalette) style(style termemu.Style) liveStyle {
	resolved := p.palette.ResolveStyle(style)
	return liveStyle{fg: resolved.Fg, bg: resolved.Bg, bold: style.Bold, dim: style.Dim, italic: style.Italic, underline: style.Underline, blink: style.Blink, strikethrough: style.Strikethrough, overline: style.Overline}
}

type liveStyle struct {
	fg            string
	bg            string
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	blink         bool
	strikethrough bool
	overline      bool
}

func (s liveStyle) sgr() string {
	parts := []string{"0"}
	if s.bold {
		parts = append(parts, "1")
	}
	if s.dim {
		parts = append(parts, "2")
	}
	if s.italic {
		parts = append(parts, "3")
	}
	if s.underline {
		parts = append(parts, "4")
	}
	if s.blink {
		parts = append(parts, "5")
	}
	if s.strikethrough {
		parts = append(parts, "9")
	}
	if s.overline {
		parts = append(parts, "53")
	}
	if r, g, b, ok := parseRGB(s.fg); ok {
		parts = append(parts, fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
	}
	if r, g, b, ok := parseRGB(s.bg); ok {
		parts = append(parts, fmt.Sprintf("48;2;%d;%d;%d", r, g, b))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func parseRGB(color string) (uint8, uint8, uint8, bool) {
	color = parseHexColor(color)
	if color == "" {
		return 0, 0, 0, false
	}
	return svg.HexToRGB(color)
}
