package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rabarbra/ttysvg/internal/app"
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ttysvg: %v\n", err)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	var cfg app.Config
	var size string
	var frameMS int
	var idleMS int
	var fontSize float64
	var fontFamily string
	var cellWidth float64
	var cellHeight float64
	var padding float64

	flags := flag.NewFlagSet("ttysvg", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.OutputPath, "o", ".", "output SVG file or directory")
	flags.StringVar(&size, "size", "", "terminal size as COLSxROWS; defaults to current terminal or 80x24")
	flags.IntVar(&frameMS, "frame-ms", 80, "minimum time between captured SVG frames")
	flags.IntVar(&idleMS, "idle-ms", 60, "capture a frame after this much output silence")
	flags.Float64Var(&fontSize, "font-size", 0, "SVG font size in px; defaults to terminal font size or 14")
	flags.StringVar(&fontFamily, "font-family", "", "SVG CSS font-family; defaults to terminal font plus Nerd Font fallbacks")
	flags.Float64Var(&cellWidth, "cell-width", 0, "SVG terminal cell width in px; defaults to font-size*0.62")
	flags.Float64Var(&cellHeight, "cell-height", 0, "SVG terminal cell height in px; defaults to font-size*1.25")
	flags.Float64Var(&padding, "padding", 12, "SVG padding in px")
	flags.StringVar(&cfg.Theme, "theme", "auto", "SVG theme: auto, dark, or light")
	flags.BoolVar(&cfg.Minify, "minify", false, "write SVG without optional whitespace")
	flags.BoolVar(&cfg.QueryTerminal, "query-terminal", true, "query current terminal colors before recording")
	flags.BoolVar(&cfg.ClearTerminal, "clear", true, "clear the terminal before recording starts")
	flags.BoolVar(&cfg.Quiet, "q", false, "do not print recording summary")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: ttysvg [flags] [--] [command [args...]]\n\n")
		fmt.Fprintf(flags.Output(), "With no command, ttysvg starts your shell in a recorder PTY. Type exit to stop recording.\n\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}

	cols, rows, err := parseSize(size)
	if err != nil {
		return 2, err
	}

	cfg.Command = flags.Args()
	cfg.Cols = cols
	cfg.Rows = rows
	cfg.FrameInterval = time.Duration(frameMS) * time.Millisecond
	cfg.IdleInterval = time.Duration(idleMS) * time.Millisecond
	cfg.FontSize = fontSize
	cfg.FontFamily = fontFamily
	cfg.CellWidth = cellWidth
	cfg.CellHeight = cellHeight
	cfg.Padding = padding

	return app.Run(context.Background(), cfg)
}

func parseSize(size string) (int, int, error) {
	if size == "" {
		return 0, 0, nil
	}
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid -size %q, expected COLSxROWS", size)
	}
	cols, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || cols <= 0 {
		return 0, 0, fmt.Errorf("invalid column count in -size %q", size)
	}
	rows, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || rows <= 0 {
		return 0, 0, fmt.Errorf("invalid row count in -size %q", size)
	}
	return cols, rows, nil
}
