package app

import (
	"os"

	"golang.org/x/term"
)

func clearInteractiveTerminal(stdout *os.File) {
	if stdout == nil || !term.IsTerminal(int(stdout.Fd())) {
		return
	}
	_, _ = stdout.WriteString("\x1b[H\x1b[2J\x1b[3J")
}
