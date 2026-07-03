package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

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
	appCursor := t.applicationCursorKeys()
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
		// In application cursor-key mode the child expects ESC O x for the
		// parameterless cursor/Home/End keys; the real terminal sends ESC [ x.
		if appCursor && isCursorKeyFinal(buf[i+2]) {
			out = append(out, 0x1b, 'O', buf[i+2])
			i += 3
			continue
		}
		out = append(out, buf[i])
		i++
	}
	return out
}

func isCursorKeyFinal(b byte) bool {
	switch b {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return true
	default:
		return false
	}
}

func handleControlKey(b byte, control *recordingControl) bool {
	if control == nil {
		return false
	}
	switch b {
	case keyToggle:
		// One key drives the whole lifecycle: start when preparing, pause
		// when recording, resume when paused.
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
		return int(seq[4]) - 32, int(seq[5]) - 32, isPressButton(int(seq[3])-32, true), true
	}
	if len(seq) < 6 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '<' {
		return 0, 0, false, false
	}
	body := string(seq[3 : len(seq)-1])
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return 0, 0, false, false
	}
	button, errB := strconv.Atoi(parts[0])
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	if errB != nil || errX != nil || errY != nil {
		return 0, 0, false, false
	}
	return x, y, isPressButton(button, seq[len(seq)-1] == 'M'), true
}

// isPressButton reports whether a mouse event is a plain button press: not a
// release (X10 encodes release as button 3; SGR uses the 'm' final), not a
// motion event (bit 32), and not a wheel event (bit 64).
func isPressButton(button int, pressFinal bool) bool {
	return pressFinal && button&3 != 3 && button&(32|64) == 0
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
	return fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", button, x, y, seq[len(seq)-1]), true
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
