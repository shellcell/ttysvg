package app

import (
	"fmt"
	"os"
	"regexp"
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
	for time.Now().Before(deadline) {
		n, err := stdin.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return parseTerminalStyleResponse(b.String())
}

func terminalColorQueries() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x1b\\")
	b.WriteString("\x1b]11;?\x1b\\")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "\x1b]4;%d;?\x1b\\", i)
	}
	return b.String()
}

func parseTerminalStyleResponse(response string) terminalStyle {
	var style terminalStyle
	for _, match := range regexp.MustCompile(`\x1b\](10|11);([^\x07\x1b]+)(?:\x07|\x1b\\)`).FindAllStringSubmatch(response, -1) {
		color := parseTerminalColor(match[2])
		if color == "" {
			continue
		}
		if match[1] == "10" {
			style.Colors.Foreground = color
		} else {
			style.Colors.Background = color
		}
	}
	for _, match := range regexp.MustCompile(`\x1b\]4;([0-9]+);([^\x07\x1b]+)(?:\x07|\x1b\\)`).FindAllStringSubmatch(response, -1) {
		idx, err := strconv.Atoi(match[1])
		if err != nil || idx < 0 || idx >= len(style.Colors.ANSI) {
			continue
		}
		if color := parseTerminalColor(match[2]); color != "" {
			style.Colors.ANSI[idx] = color
		}
	}
	style.Theme = themeFromBackground(style.Colors.Background)
	return style
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
