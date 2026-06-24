package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rabarbra/ttysvg/internal/eventlog"
	"github.com/rabarbra/ttysvg/internal/ptyrec"
	"github.com/rabarbra/ttysvg/internal/svg"
	"github.com/rabarbra/ttysvg/internal/terminal"
	"golang.org/x/term"
)

type Config struct {
	OutputPath    string
	Command       []string
	Cols          int
	Rows          int
	FrameInterval time.Duration
	IdleInterval  time.Duration
	Theme         string
	FontSize      float64
	FontFamily    string
	Colors        svg.Colors
	CellWidth     float64
	CellHeight    float64
	Padding       float64
	Minify        bool
	QueryTerminal bool
	ClearTerminal bool
	Quiet         bool
}

func Run(ctx context.Context, cfg Config) (int, error) {
	themeAuto := cfg.Theme == "" || cfg.Theme == "auto"
	if err := cfg.setDefaults(); err != nil {
		return 2, err
	}
	outputPath, err := prepareOutputPath(cfg.OutputPath, os.Stdin, os.Stderr)
	if err != nil {
		return 2, err
	}
	cfg.OutputPath = outputPath
	if cfg.QueryTerminal {
		queried := queryTerminalStyle(os.Stdin, os.Stdout, 120*time.Millisecond)
		if !queried.empty() {
			merged := terminalStyle{Colors: cfg.Colors}
			merged.merge(queried)
			cfg.Colors = merged.Colors
			if themeAuto && queried.Theme != "" {
				cfg.Theme = queried.Theme
			}
		}
	}
	if cfg.ClearTerminal {
		clearInteractiveTerminal(os.Stdout)
	}

	logFile, err := os.CreateTemp("", "ttysvg-*.ttylog")
	if err != nil {
		return 1, fmt.Errorf("create temp event log: %w", err)
	}
	logPath := logFile.Name()
	defer os.Remove(logPath)

	writer := eventlog.NewWriter(logFile)
	if !cfg.Quiet && !cfg.ClearTerminal {
		fmt.Fprintf(os.Stderr, "ttysvg: recording to %s; type exit to stop\n", cfg.OutputPath)
	}
	recorder := ptyrec.Recorder{
		Command: cfg.Command,
		Cols:    cfg.Cols,
		Rows:    cfg.Rows,
		Stdout:  os.Stdout,
		Stdin:   os.Stdin,
		Stderr:  os.Stderr,
	}

	recordStart := time.Now()
	exitCode, recordErr := recorder.Run(ctx, writer)
	if err := writer.Close(); err != nil && recordErr == nil {
		recordErr = fmt.Errorf("close event log: %w", err)
	}
	if err := logFile.Close(); err != nil && recordErr == nil {
		recordErr = fmt.Errorf("close temp event log: %w", err)
	}
	if recordErr != nil {
		return exitCodeOrOne(exitCode), recordErr
	}

	stats, err := render(ctx, cfg, logPath)
	if err != nil {
		return 1, err
	}

	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "ttysvg: wrote %s (%s, %d frames, %s recorded, %s total)\n",
			cfg.OutputPath, formatBytes(stats.Size), stats.Frames, stats.Duration.Round(time.Millisecond), time.Since(recordStart).Round(time.Millisecond))
	}
	return exitCode, nil
}

func (cfg *Config) setDefaults() error {
	detected := detectTerminalStyle()
	if cfg.OutputPath == "" {
		cfg.OutputPath = "."
	}
	if len(cfg.Command) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cfg.Command = []string{shell}
	}
	if cfg.Cols == 0 || cfg.Rows == 0 {
		cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil || cols <= 0 || rows <= 0 {
			cols, rows = 80, 24
		}
		if cfg.Cols == 0 {
			cfg.Cols = cols
		}
		if cfg.Rows == 0 {
			cfg.Rows = rows
		}
	}
	if cfg.Cols <= 0 || cfg.Rows <= 0 {
		return errors.New("terminal size must be positive")
	}
	if cfg.FrameInterval <= 0 {
		return errors.New("-frame-ms must be positive")
	}
	if cfg.IdleInterval < 0 {
		return errors.New("-idle-ms cannot be negative")
	}
	if cfg.FontFamily == "" && detected.FontFamily != "" {
		cfg.FontFamily = cssFontFamilyWithFallback(detected.FontFamily)
	}
	if cfg.FontSize == 0 && detected.FontSize > 0 {
		cfg.FontSize = detected.FontSize
	}
	if cfg.FontSize == 0 {
		cfg.FontSize = 14
	}
	if cfg.FontSize < 0 {
		return errors.New("-font-size must be positive")
	}
	if cfg.Theme == "" || cfg.Theme == "auto" {
		cfg.Theme = detected.Theme
		if cfg.Theme == "" {
			cfg.Theme = "dark"
		}
	}
	if colorsEmpty(cfg.Colors) && !colorsEmpty(detected.Colors) {
		cfg.Colors = detected.Colors
	}
	if cfg.CellWidth < 0 || cfg.CellHeight < 0 || cfg.Padding < 0 {
		return errors.New("SVG dimensions cannot be negative")
	}
	if cfg.Theme != "dark" && cfg.Theme != "light" {
		return fmt.Errorf("unsupported theme %q", cfg.Theme)
	}
	return nil
}

type renderStats struct {
	Frames   int
	Duration time.Duration
	Size     int64
}

func render(ctx context.Context, cfg Config, logPath string) (renderStats, error) {
	in, err := os.Open(logPath)
	if err != nil {
		return renderStats{}, fmt.Errorf("open event log: %w", err)
	}
	defer in.Close()
	logStat, err := in.Stat()
	if err != nil {
		return renderStats{}, fmt.Errorf("stat event log: %w", err)
	}
	counter := &countingReader{r: in}

	out, err := createOutput(cfg.OutputPath)
	if err != nil {
		return renderStats{}, err
	}
	defer out.cleanup()

	bufferedOut := bufio.NewWriterSize(out.file, 256*1024)
	renderer := svg.NewRenderer(bufferedOut, svg.Config{
		Cols:       cfg.Cols,
		Rows:       cfg.Rows,
		Theme:      cfg.Theme,
		FontSize:   cfg.FontSize,
		FontFamily: cfg.FontFamily,
		Colors:     cfg.Colors,
		CellWidth:  cfg.CellWidth,
		CellHeight: cfg.CellHeight,
		Padding:    cfg.Padding,
		Minify:     cfg.Minify,
	})
	if err := renderer.Begin(); err != nil {
		return renderStats{}, err
	}

	screen := terminal.NewScreen(cfg.Cols, cfg.Rows)
	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}
	if info, ok := terminal.LoadTerminfo(termName); ok {
		screen.SetTerminfo(info)
	}
	reader := eventlog.NewReader(counter)
	progress := newProgressBar(logStat.Size(), os.Stderr, cfg.Quiet)
	progress.Start()
	stats, err := replay(ctx, reader, screen, renderer, cfg.FrameInterval, cfg.IdleInterval, func() {
		progress.Update(counter.n)
	})
	progress.Finish()
	if err != nil {
		return renderStats{}, err
	}
	if err := renderer.End(); err != nil {
		return renderStats{}, err
	}
	if err := bufferedOut.Flush(); err != nil {
		return renderStats{}, fmt.Errorf("flush SVG: %w", err)
	}
	if err := out.close(); err != nil {
		return renderStats{}, err
	}
	outputStat, err := os.Stat(cfg.OutputPath)
	if err != nil {
		return renderStats{}, fmt.Errorf("stat output file: %w", err)
	}
	stats.Size = outputStat.Size()
	return stats, nil
}

type frameWriter interface {
	WriteFrame(frame terminal.Frame, begin time.Duration, duration time.Duration) error
	WriteFinalFrame(frame terminal.Frame, begin time.Duration) error
	FrameCount() int
}

func replay(ctx context.Context, reader *eventlog.Reader, screen *terminal.Screen, writer frameWriter, frameInterval, idleInterval time.Duration, onProgress func()) (renderStats, error) {
	prevFrame := screen.Snapshot()
	defer prevFrame.Release()
	prevFrameAt := time.Duration(0)
	lastSnapshotAt := time.Duration(0)
	lastEventAt := time.Duration(0)
	dirty := false
	lastRecordAt := time.Duration(0)

	capture := func(at time.Duration) error {
		frame := screen.Snapshot()
		if frame.Equal(prevFrame) {
			frame.Release()
			lastSnapshotAt = at
			dirty = false
			return nil
		}
		if at > prevFrameAt {
			if err := writer.WriteFrame(prevFrame, prevFrameAt, at-prevFrameAt); err != nil {
				frame.Release()
				return err
			}
		}
		prevFrame.Release()
		prevFrame = frame
		prevFrameAt = at
		lastSnapshotAt = at
		dirty = false
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return renderStats{}, ctx.Err()
		default:
		}

		record, err := reader.Next()
		if onProgress != nil {
			onProgress()
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return renderStats{}, fmt.Errorf("read event log: %w", err)
		}

		if dirty && idleInterval > 0 && record.At-lastEventAt >= idleInterval {
			if err := capture(lastEventAt); err != nil {
				return renderStats{}, err
			}
		}

		if screen.Write(record.Data) {
			dirty = true
		}
		lastEventAt = record.At
		lastRecordAt = record.At

		if dirty && record.At-lastSnapshotAt >= frameInterval && (idleInterval == 0 || !screen.AlternateActive()) {
			if err := capture(record.At); err != nil {
				return renderStats{}, err
			}
		}
	}

	if dirty {
		if err := capture(lastEventAt); err != nil {
			return renderStats{}, err
		}
	}
	if err := writer.WriteFinalFrame(prevFrame, prevFrameAt); err != nil {
		return renderStats{}, err
	}

	return renderStats{Frames: writer.FrameCount(), Duration: lastRecordAt}, nil
}

func exitCodeOrOne(code int) int {
	if code != 0 {
		return code
	}
	return 1
}

type outputFile struct {
	file      *os.File
	path      string
	tempPath  string
	committed bool
}

func createOutput(path string) (*outputFile, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "."+base+"-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	return &outputFile{file: file, path: path, tempPath: file.Name()}, nil
}

func (f *outputFile) close() error {
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("close output file: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(f.path)
	}
	if err := os.Rename(f.tempPath, f.path); err != nil {
		return fmt.Errorf("move output into place: %w", err)
	}
	f.committed = true
	return nil
}

func (f *outputFile) cleanup() {
	if f == nil || f.committed {
		return
	}
	_ = f.file.Close()
	_ = os.Remove(f.tempPath)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}
