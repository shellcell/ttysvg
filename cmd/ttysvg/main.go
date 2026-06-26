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

var version = "0.0.6"

// retiredFlags maps removed flag names to migration guidance. They are detected
// before parsing so users get a pointed message instead of a bare "not defined".
var retiredFlags = map[string]string{
	"cols":              "use -size instead, e.g. -size 100x",
	"rows":              "use -size instead, e.g. -size x30",
	"frame-ms":          "use -frame with a duration, e.g. -frame 80ms",
	"idle-ms":           "use -idle with a duration, e.g. -idle 60ms",
	"clear":             "clearing is on by default; pass -no-clear to disable it",
	"no-query-terminal": "terminal querying is off by default; remove this flag, or pass -query-terminal to enable terminal detection",
	"minify":            "SVG output is always compact now, so this flag is no longer needed",
}

// valueFlags are the flags that consume the following argument when written as
// "-flag value" (rather than "-flag=value"). Used to skip values while scanning
// for retired flags so a value like "100x" is not mistaken for a flag.
var valueFlags = map[string]bool{
	"o": true, "size": true, "frame": true, "idle": true,
	"font-size": true, "font-family": true, "cell-width": true,
	"cell-height": true, "padding": true, "theme": true, "bg": true,
}

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
	var frame time.Duration
	var idle time.Duration
	var fontSize float64
	var fontFamily string
	var cellWidth float64
	var cellHeight float64
	var padding float64
	var queryTerminal bool
	var noClear bool
	var showVersion bool

	flags := flag.NewFlagSet("ttysvg", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.OutputPath, "o", ".", "output SVG file or directory")
	flags.StringVar(&size, "size", "", "recording size as COLSxROWS; omit either side to auto-fit the terminal (100x, x30, 100x30)")
	flags.DurationVar(&frame, "frame", 80*time.Millisecond, "minimum time between captured SVG frames, e.g. 80ms")
	flags.DurationVar(&idle, "idle", 60*time.Millisecond, "capture a frame after this much output silence; 0 disables")
	flags.Float64Var(&fontSize, "font-size", 0, "SVG output font size in px; does not change the live terminal font; defaults to detected terminal font size with -query-terminal, otherwise 14")
	flags.StringVar(&fontFamily, "font-family", "", "SVG CSS font-family; defaults to terminal font plus Nerd Font fallbacks")
	flags.Float64Var(&cellWidth, "cell-width", 0, "SVG terminal cell width in px; defaults to font-size*0.62")
	flags.Float64Var(&cellHeight, "cell-height", 0, "SVG terminal cell height in px; defaults to font-size*1.25")
	flags.Float64Var(&padding, "padding", 0, "SVG padding in px")
	flags.StringVar(&cfg.Theme, "theme", "auto", "SVG theme: auto, dark, or light")
	flags.StringVar(&cfg.Background, "bg", "", "terminal background color during recording, e.g. #0d1117; also used as SVG background")
	flags.BoolVar(&queryTerminal, "query-terminal", false, "query/identify the current terminal for colors, theme, and font before recording; slower startup")
	flags.BoolVar(&noClear, "no-clear", false, "do not clear the terminal before recording starts")
	flags.BoolVar(&cfg.Quiet, "q", false, "do not print recording summary")
	flags.BoolVar(&showVersion, "version", false, "print version and exit")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: ttysvg [flags] [--] [command [args...]]\n\n")
		fmt.Fprintf(out, "With no command, ttysvg starts your shell in a recorder PTY. Type exit to stop recording.\n\n")
		fmt.Fprintf(out, "Output:\n")
		printFlags(flags, "o", "q")
		fmt.Fprintf(out, "\nRecording:\n")
		printFlags(flags, "size", "frame", "idle", "no-clear", "query-terminal", "bg")
		fmt.Fprintf(out, "\nAppearance:\n")
		printFlags(flags, "theme", "font-size", "font-family", "cell-width", "cell-height", "padding")
		fmt.Fprintf(out, "\nOther:\n")
		printFlags(flags, "version")
	}

	if name, guidance, ok := findRetiredFlag(args); ok {
		return 2, fmt.Errorf("-%s has been removed: %s", name, guidance)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	if showVersion {
		fmt.Fprintf(os.Stdout, "ttysvg %s\n", version)
		return 0, nil
	}

	cols, rows, err := parseSize(size)
	if err != nil {
		return 2, err
	}

	cfg.Command = flags.Args()
	cfg.Cols = cols
	cfg.Rows = rows
	cfg.FixedSize = size != ""
	cfg.FrameInterval = frame
	cfg.IdleInterval = idle
	cfg.FontSize = fontSize
	cfg.FontFamily = fontFamily
	cfg.CellWidth = cellWidth
	cfg.CellHeight = cellHeight
	cfg.Padding = padding
	cfg.QueryTerminal = queryTerminal
	cfg.ClearTerminal = !noClear

	return app.Run(context.Background(), cfg)
}

func printFlags(flags *flag.FlagSet, names ...string) {
	out := flags.Output()
	for _, name := range names {
		f := flags.Lookup(name)
		if f == nil {
			continue
		}
		fmt.Fprintf(out, "  -%-18s %s\n", name, f.Usage)
	}
}

// findRetiredFlag scans the flag portion of args (everything before the command
// or "--") for a removed flag, skipping the values of value-taking flags.
func findRetiredFlag(args []string) (string, string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			break // first bare argument starts the command
		}
		name := strings.TrimLeft(arg, "-")
		hasInlineValue := strings.Contains(name, "=")
		name, _, _ = strings.Cut(name, "=")
		if guidance, ok := retiredFlags[name]; ok {
			return name, guidance, true
		}
		if !hasInlineValue && valueFlags[name] {
			i++ // the next argument is this flag's value, not a flag
		}
	}
	return "", "", false
}

// parseSize parses COLSxROWS where either side may be omitted to mean "use the
// detected terminal dimension". A zero return for a dimension means unspecified.
func parseSize(size string) (int, int, error) {
	if size == "" {
		return 0, 0, nil
	}
	lower := strings.ToLower(strings.TrimSpace(size))
	left, right, found := strings.Cut(lower, "x")
	if !found {
		return 0, 0, fmt.Errorf("invalid -size %q, expected COLSxROWS (either side may be omitted, e.g. 100x or x30)", size)
	}
	cols, err := parseSizePart(left)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid column count in -size %q", size)
	}
	rows, err := parseSizePart(right)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid row count in -size %q", size)
	}
	if cols == 0 && rows == 0 {
		return 0, 0, fmt.Errorf("invalid -size %q, specify at least a width or a height", size)
	}
	return cols, rows, nil
}

func parseSizePart(part string) (int, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(part)
	if err != nil || n <= 0 {
		return 0, errors.New("invalid")
	}
	return n, nil
}
