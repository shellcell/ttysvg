package app

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func applyTerminalBackground(stdout *os.File, bg string) {
	if stdout == nil || !term.IsTerminal(int(stdout.Fd())) {
		return
	}
	bg = parseHexColor(bg)
	if bg == "" {
		return
	}
	_, _ = fmt.Fprintf(stdout, "\x1b]11;%s\x1b\\", bg)
}

func restoreTerminalBackground(stdout *os.File) {
	if stdout == nil || !term.IsTerminal(int(stdout.Fd())) {
		return
	}
	_, _ = fmt.Fprintf(stdout, "\x1b]111\x1b\\")
}
