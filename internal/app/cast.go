package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rabarbra/ttysvg/internal/eventlog"
)

// castHeader is the first line of an asciinema recording. v2 stores the size
// as width/height; v3 nests it under term.
type castHeader struct {
	Version       int     `json:"version"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	IdleTimeLimit float64 `json:"idle_time_limit"`
	Term          struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	} `json:"term"`
}

func (h castHeader) size() (int, int) {
	if h.Version >= 3 {
		return h.Term.Cols, h.Term.Rows
	}
	return h.Width, h.Height
}

// convertCast reads an asciinema v2/v3 cast stream and writes its output
// events into the ttysvg event log. It returns the recorded terminal size.
// v2 event timestamps are absolute; v3 timestamps are intervals since the
// previous event. An idle_time_limit in the header caps silent gaps the same
// way asciinema players do.
func convertCast(r io.Reader, writer *eventlog.Writer, warn io.Writer) (int, int, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, 0, fmt.Errorf("read cast header: %w", err)
		}
		return 0, 0, errors.New("cast file is empty")
	}
	var header castHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return 0, 0, fmt.Errorf("parse cast header: %w", err)
	}
	if header.Version != 2 && header.Version != 3 {
		return 0, 0, fmt.Errorf("unsupported cast version %d (v2 and v3 are supported)", header.Version)
	}
	cols, rows := header.size()
	if cols <= 0 || rows <= 0 {
		return 0, 0, errors.New("cast header has no terminal size")
	}

	idleLimit := time.Duration(header.IdleTimeLimit * float64(time.Second))
	var at, lastRaw time.Duration
	warned := false
	line := 1
	for scanner.Scan() {
		line++
		text := scanner.Bytes()
		if len(text) == 0 {
			continue
		}
		var event []json.RawMessage
		if err := json.Unmarshal(text, &event); err != nil {
			return 0, 0, fmt.Errorf("parse cast event on line %d: %w", line, err)
		}
		if len(event) < 3 {
			return 0, 0, fmt.Errorf("cast event on line %d has %d fields, want 3", line, len(event))
		}
		var stamp float64
		var kind, data string
		if err := json.Unmarshal(event[0], &stamp); err != nil {
			return 0, 0, fmt.Errorf("parse cast timestamp on line %d: %w", line, err)
		}
		if err := json.Unmarshal(event[1], &kind); err != nil {
			return 0, 0, fmt.Errorf("parse cast event type on line %d: %w", line, err)
		}
		if err := json.Unmarshal(event[2], &data); err != nil {
			return 0, 0, fmt.Errorf("parse cast event data on line %d: %w", line, err)
		}

		gap := time.Duration(stamp * float64(time.Second))
		if header.Version == 2 {
			// Absolute timestamps: convert to a gap so the idle cap applies.
			gap, lastRaw = gap-lastRaw, gap
		}
		if gap < 0 {
			gap = 0
		}
		if idleLimit > 0 && gap > idleLimit {
			gap = idleLimit
		}
		at += gap

		switch kind {
		case "o":
			if err := writer.WriteOutput(at, []byte(data)); err != nil {
				return 0, 0, fmt.Errorf("write event log: %w", err)
			}
		case "r":
			if !warned && warn != nil {
				fmt.Fprintf(warn, "ttysvg: cast resizes the terminal mid-recording (line %d); output keeps the initial %dx%d grid\n", line, cols, rows)
				warned = true
			}
		default:
			// Input, markers, and exit events do not affect the rendered screen.
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("read cast file: %w", err)
	}
	return cols, rows, nil
}

// runCast converts an existing asciinema cast to SVG using the same replay and
// render pipeline as live recordings.
func runCast(ctx context.Context, cfg Config, themeAuto bool) (int, error) {
	in, err := os.Open(cfg.CastPath)
	if err != nil {
		return 1, fmt.Errorf("open cast file: %w", err)
	}
	defer in.Close()

	logFile, err := os.CreateTemp("", "ttysvg-*.ttylog")
	if err != nil {
		return 1, fmt.Errorf("create temp event log: %w", err)
	}
	logPath := logFile.Name()
	defer os.Remove(logPath)

	writer := eventlog.NewWriter(logFile)
	var warn io.Writer
	if !cfg.Quiet {
		warn = os.Stderr
	}
	cols, rows, err := convertCast(in, writer, warn)
	if err == nil {
		err = writer.Close()
	}
	if closeErr := logFile.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close temp event log: %w", closeErr)
	}
	if err != nil {
		return 1, err
	}

	// An explicit -size wins; otherwise the cast header defines the grid.
	if cfg.Cols == 0 {
		cfg.Cols = cols
	}
	if cfg.Rows == 0 {
		cfg.Rows = rows
	}
	if err := cfg.setDefaults(); err != nil {
		return 2, err
	}
	if cfg.QueryTerminal {
		queried := queryTerminalStyle(os.Stdin, os.Stdout, 500*time.Millisecond)
		if !queried.empty() {
			merged := terminalStyle{Colors: cfg.Colors}
			merged.merge(queried)
			cfg.Colors = merged.Colors
			if themeAuto && queried.Theme != "" {
				cfg.Theme = queried.Theme
			}
			if cfg.FontFamily == "" && queried.FontFamily != "" {
				cfg.FontFamily = cssFontFamilyWithFallback(queried.FontFamily)
			}
			if cfg.FontSize == 0 && queried.FontSize > 0 {
				cfg.FontSize = queried.FontSize
			}
		}
	}
	if cfg.FontSize == 0 {
		cfg.FontSize = 14
	}
	cfg.Gzip = cfg.Gzip || strings.HasSuffix(strings.ToLower(cfg.OutputPath), ".svgz")
	outputPath, err := prepareOutputPath(cfg.OutputPath, os.Stdin, os.Stderr)
	if err != nil {
		return 2, err
	}
	if cfg.Gzip && outputPath != "-" {
		outputPath = svgzPath(outputPath)
	}
	cfg.OutputPath = outputPath
	if err := cfg.applyBackgroundOverride(); err != nil {
		return 2, err
	}

	start := time.Now()
	stats, err := render(ctx, cfg, logPath, writer.LastAt())
	if err != nil {
		return 1, err
	}
	if !cfg.Quiet {
		target := cfg.OutputPath
		if target == "-" {
			target = "stdout"
		}
		fmt.Fprintf(os.Stderr, "ttysvg: wrote %s (%s, %d frames, %s cast, %s total)\n",
			target, formatBytes(stats.Size), stats.Frames, stats.Duration.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
	}
	return 0, nil
}
