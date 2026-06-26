package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

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
	// Save the application cursor before repainting controls and restore it
	// afterwards (DECSC/DECRC). Without this the physical cursor is left in the
	// status bar after every redraw, so the pane prompt appears to lose its
	// cursor until the next byte of child output repositions it.
	b.WriteString("\x1b7")
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
	b.WriteString("\x1b8")
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
		fmt.Fprintf(&b, "PREP %dx%d ", cfg.Cols, cfg.Rows)
	}
	// Keep the keyboard hints visible in every state, not just while preparing.
	if hint := controlKeyHint(state); hint != "" {
		fmt.Fprintf(&b, "| %s ", hint)
	}
	return b.String()
}

func controlKeyHint(state recordingState) string {
	switch state {
	case recordingActive:
		return "Ctrl-P pause Ctrl-Q stop"
	case recordingPaused:
		return "Ctrl-R resume Ctrl-Q stop"
	case recordingStopped:
		return ""
	default:
		return "Ctrl-R start Ctrl-Q stop"
	}
}

func sideControlLines(cfg Config, state recordingState, elapsed time.Duration) []string {
	lines := []string{"ttysvg", fmt.Sprintf("%dx%d", cfg.Cols, cfg.Rows), ""}
	for _, button := range controlButtons(state) {
		lines = append(lines, button.label, "")
	}
	switch state {
	case recordingActive:
		lines = append(lines, "REC", formatRecordClock(elapsed), "", "^P pause", "^Q stop")
	case recordingPaused:
		lines = append(lines, "PAUSED", formatRecordClock(elapsed), "", "^R resume", "^Q stop")
	case recordingStopped:
		lines = append(lines, "STOP")
	default:
		lines = append(lines, "PREP", "", "^R start", "^Q stop")
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

// Pane rendering is coalesced to at most one repaint per flush interval so a
// burst of PTY reads does not trigger a full snapshot+diff+write per read. The
