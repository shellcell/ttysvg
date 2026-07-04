package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

func queryTerminalStyle(stdin *os.File, stdout *os.File, timeout time.Duration) terminalStyle {
	if stdin == nil || stdout == nil || timeout <= 0 {
		return terminalStyle{}
	}
	if !term.IsTerminal(int(stdin.Fd())) || !term.IsTerminal(int(stdout.Fd())) {
		return terminalStyle{}
	}
	state, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		return terminalStyle{}
	}
	defer term.Restore(int(stdin.Fd()), state)

	deadline := time.Now().Add(timeout)
	if err := stdin.SetReadDeadline(deadline); err != nil {
		return terminalStyle{}
	}
	defer stdin.SetReadDeadline(time.Time{})

	if _, err := stdout.WriteString(terminalColorQueries()); err != nil {
		return terminalStyle{}
	}

	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
			// The trailing DA1 query is answered by effectively every terminal,
			// and replies arrive in request order, so its reply marks the end of
			// the color/font reports: stop immediately instead of waiting out
			// the full timeout.
			if hasDA1Reply(b.String()) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	return parseTerminalStyleResponse(b.String())
}

// terminalColorQueries asks for the default foreground/background (OSC 10/11),
// the 16 ANSI palette entries (OSC 4), the font (OSC 50, xterm and a few
// others; silently ignored elsewhere), and finally primary device attributes
// (DA1) as an end-of-replies sentinel.
func terminalColorQueries() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x1b\\")
	b.WriteString("\x1b]11;?\x1b\\")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "\x1b]4;%d;?\x1b\\", i)
	}
	b.WriteString("\x1b]50;?\x1b\\")
	b.WriteString("\x1b[c")
	return b.String()
}

// hasDA1Reply reports whether s contains a DA1 response (CSI ? ... c).
func hasDA1Reply(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' || s[i+2] != '?' {
			continue
		}
		for j := i + 3; j < len(s); j++ {
			c := s[j]
			if c == 'c' {
				return true
			}
			if !(c >= '0' && c <= '9') && c != ';' {
				break
			}
		}
	}
	return false
}

func parseTerminalStyleResponse(response string) terminalStyle {
	var style terminalStyle
	for _, fields := range parseOSCSequences(response) {
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "10", "11":
			color := parseTerminalColor(strings.Join(fields[1:], ";"))
			if color == "" {
				continue
			}
			if fields[0] == "10" {
				style.Colors.Foreground = color
			} else {
				style.Colors.Background = color
			}
		case "4":
			if len(fields) < 3 {
				continue
			}
			idx, err := strconv.Atoi(fields[1])
			if err != nil || idx < 0 || idx >= len(style.Colors.ANSI) {
				continue
			}
			if color := parseTerminalColor(strings.Join(fields[2:], ";")); color != "" {
				style.Colors.ANSI[idx] = color
			}
		case "50":
			family, size := parseFontReport(strings.Join(fields[1:], ";"))
			if family != "" {
				style.FontFamily = family
			}
			if size > 0 {
				style.FontSize = size
			}
		}
	}
	style.Theme = themeFromBackground(style.Colors.Background)
	return style
}

// parseFontReport extracts a font family and pixel size from an OSC 50 reply.
// Terminals report either an XLFD name
// (-foundry-family-weight-...-pixel-point-...) or a fontconfig-style pattern
// ("JetBrains Mono:pixelsize=14", optionally with an "xft:" prefix).
func parseFontReport(value string) (string, float64) {
	value = strings.TrimSpace(value)
	if value == "" || value == "?" {
		return "", 0
	}
	if strings.HasPrefix(value, "-") {
		parts := strings.Split(value, "-")
		if len(parts) < 3 {
			return "", 0
		}
		family := strings.TrimSpace(parts[2])
		var size float64
		if len(parts) > 7 {
			if px, err := strconv.ParseFloat(parts[7], 64); err == nil && px > 0 {
				size = px
			}
		}
		return family, size
	}
	value = strings.TrimPrefix(value, "xft:")
	parts := strings.Split(value, ":")
	family := strings.TrimSpace(parts[0])
	var size float64
	for _, part := range parts[1:] {
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil || f <= 0 {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "pixelsize":
			size = f
		case "size":
			// Points; approximate px at the CSS 96dpi ratio.
			if size == 0 {
				size = f * 4 / 3
			}
		}
	}
	return family, size
}

// parseOSCSequences scans s for OSC sequences of the form
// ESC ] body BEL  or  ESC ] body ESC \, returning each body split on ';'.
// The body runs up to the first BEL or ESC, matching the terminal color
// reports queried in terminalColorQueries.
func parseOSCSequences(s string) [][]string {
	var out [][]string
	for i := 0; i < len(s); {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != ']' {
			i++
			continue
		}
		j := i + 2
		start := j
		for j < len(s) && s[j] != 0x07 && s[j] != 0x1b {
			j++
		}
		out = append(out, strings.Split(s[start:j], ";"))
		switch {
		case j < len(s) && s[j] == 0x07:
			j++
		case j+1 < len(s) && s[j] == 0x1b && s[j+1] == '\\':
			j += 2
		}
		i = j
	}
	return out
}

func parseTerminalColor(value string) string {
	value = strings.TrimSpace(value)
	if color := parseHexColor(value); color != "" {
		return color
	}
	if !strings.HasPrefix(strings.ToLower(value), "rgb:") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(value, "rgb:"), "RGB:"), "/")
	if len(parts) != 3 {
		return ""
	}
	var rgb [3]uint8
	for i, part := range parts {
		component, ok := parseTerminalColorComponent(part)
		if !ok {
			return ""
		}
		rgb[i] = component
	}
	return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
}

func parseTerminalColorComponent(value string) (uint8, bool) {
	if value == "" || len(value) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(value, 16, 16)
	if err != nil {
		return 0, false
	}
	if len(value) <= 2 {
		if len(value) == 1 {
			v *= 17
		}
		return uint8(v), true
	}
	max := uint64(1<<(4*len(value))) - 1
	return uint8((v*255 + max/2) / max), true
}
