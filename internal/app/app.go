package app

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shellcell/ttysvg/internal/eventlog"
	"github.com/shellcell/ttysvg/internal/ptyrec"
	"github.com/shellcell/ttysvg/internal/svg"
	"github.com/shellcell/ttysvg/internal/terminal"
	"golang.org/x/term"
)

type Config struct {
	OutputPath    string
	Command       []string
	Cols          int
	Rows          int
	FixedSize     bool
	FrameInterval time.Duration
	IdleInterval  time.Duration
	Theme         string
	Background    string
	FontSize      float64
	FontFamily    string
	Colors        svg.Colors
	CellWidth     float64
	CellHeight    float64
	Padding       float64
	QueryTerminal bool
	ClearTerminal bool
	Quiet         bool
	// Autostart begins recording immediately in pane mode instead of waiting for
	// the Ctrl-R control. It has no effect in direct mode, which already records
	// from the start.
	Autostart bool
	// Headless skips the interactive pane entirely and records the requested size
	// directly, even on an interactive terminal. Intended for scripting and CI.
	Headless bool
	// NoLoop disables the default infinite loop so the SVG plays once and freezes
	// on the final screen.
	NoLoop bool
	// EndHold is how long the final screen is held before the loop repeats.
	// Zero means the built-in default (2s).
	EndHold time.Duration
	// CastPath converts an existing asciinema .cast recording instead of
	// recording a live session.
	CastPath string
	// Gzip writes a gzip-compressed .svgz file instead of a plain .svg. It is also
	// enabled automatically when the output path ends in .svgz.
	Gzip bool
}

func Run(ctx context.Context, cfg Config) (int, error) {
	themeAuto := cfg.Theme == "" || cfg.Theme == "auto"
	if cfg.CastPath != "" {
		return runCast(ctx, cfg, themeAuto)
	}
	if err := cfg.setDefaults(); err != nil {
		return 2, err
	}
	// A .svgz output path implies gzip; the -gz flag forces it regardless of
	// extension. Either way the resolved path is normalized to end in .svgz.
	cfg.Gzip = cfg.Gzip || strings.HasSuffix(strings.ToLower(cfg.OutputPath), ".svgz")
	outputPath, err := prepareOutputPath(cfg.OutputPath, os.Stdin, os.Stderr)
	if err != nil {
		return 2, err
	}
	toStdout := outputPath == "-"
	if cfg.Gzip && !toStdout {
		outputPath = svgzPath(outputPath)
	}
	cfg.OutputPath = outputPath
	// With -o - the SVG owns stdout, so the live child output moves to stderr
	// (in CI that keeps the log visible; in a terminal with stdout redirected
	// the session stays interactive).
	liveOut := os.Stdout
	if toStdout {
		liveOut = os.Stderr
	}
	if cfg.QueryTerminal {
		// The DA1 sentinel ends the read as soon as all replies are in, so the
		// timeout is only a ceiling for unresponsive terminals.
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
	if err := cfg.applyBackgroundOverride(); err != nil {
		return 2, err
	}
	liveTerminal, err := setupLiveTerminal(liveOut, cfg)
	if err != nil {
		return 2, err
	}
	if cfg.ClearTerminal && !liveTerminal.Decorated() {
		clearInteractiveTerminal(liveOut)
	}
	liveTerminal.Activate()
	defer liveTerminal.Restore()

	logFile, err := os.CreateTemp("", "ttysvg-*.ttylog")
	if err != nil {
		return 1, fmt.Errorf("create temp event log: %w", err)
	}
	logPath := logFile.Name()
	defer os.Remove(logPath)

	writer := eventlog.NewWriter(logFile)
	if !cfg.Quiet && !cfg.ClearTerminal && !liveTerminal.Decorated() {
		fmt.Fprintf(os.Stderr, "ttysvg: recording to %s; type exit to stop\n", cfg.OutputPath)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var control *recordingControl
	sink := ptyrec.Sink(writer)
	var input io.Reader
	if liveTerminal.Decorated() {
		control = newRecordingControl(writer, liveTerminal, cancel)
		defer control.Close()
		sink = control
		input = newPaneInputReader(os.Stdin, liveTerminal, control)
		if cfg.Autostart {
			control.StartOrResume()
		}
	}

	// A termination signal must not kill ttysvg mid-session: that would leave
	// the terminal in raw mode (and pane mode in the alternate screen) and lose
	// the recording. Treat the first signal as "stop recording and render what
	// we have"; after that, signals regain their default behavior.
	var signalStop atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()
	go func() {
		if sig := <-sigCh; sig == nil {
			return // channel closed on normal shutdown
		}
		signalStop.Store(true)
		signal.Stop(sigCh)
		if control != nil {
			control.Stop()
		} else {
			cancel()
		}
	}()
	recorder := ptyrec.Recorder{
		Command: cfg.Command,
		Cols:    cfg.Cols,
		Rows:    cfg.Rows,
		Stdout:  liveTerminal.Writer(),
		Stdin:   os.Stdin,
		Input:   input,
		Stderr:  os.Stderr,
	}

	recordStart := time.Now()
	exitCode, recordErr := recorder.Run(runCtx, sink)
	liveTerminal.Restore()
	stopRequested := signalStop.Load() || (control != nil && control.StopRequested())
	if stopRequested && recordErr != nil {
		recordErr = nil
		exitCode = 0
	}
	if control != nil && control.Err() != nil && recordErr == nil {
		recordErr = control.Err()
	}
	if err := writer.Close(); err != nil && recordErr == nil {
		recordErr = fmt.Errorf("close event log: %w", err)
	}
	if err := logFile.Close(); err != nil && recordErr == nil {
		recordErr = fmt.Errorf("close temp event log: %w", err)
	}
	if recordErr != nil {
		return exitCodeOrOne(exitCode), recordErr
	}
	if control != nil && !cfg.Quiet {
		for _, path := range control.Snapshots() {
			fmt.Fprintf(os.Stderr, "ttysvg: wrote snapshot %s\n", path)
		}
	}
	if control != nil && !control.Started() {
		if !cfg.Quiet {
			fmt.Fprintf(os.Stderr, "ttysvg: recording was not started; no animation written\n")
		}
		return exitCode, nil
	}

	// Looping needs the total recording length up front so each reveal can be
	// timed as a fraction of the loop period; the writer tracked it as it wrote.
	stats, err := render(ctx, cfg, logPath, writer.LastAt())
	if err != nil {
		return 1, err
	}

	if !cfg.Quiet {
		target := cfg.OutputPath
		if target == "-" {
			target = "stdout"
		}
		fmt.Fprintf(os.Stderr, "ttysvg: wrote %s (%s, %d frames, %s recorded, %s total)\n",
			target, formatBytes(stats.Size), stats.Frames, stats.Duration.Round(time.Millisecond), time.Since(recordStart).Round(time.Millisecond))
	}
	return exitCode, nil
}

func (cfg *Config) setDefaults() error {
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
		return errors.New("-frame must be positive")
	}
	if cfg.IdleInterval < 0 {
		return errors.New("-idle cannot be negative")
	}
	if cfg.FontSize < 0 {
		return errors.New("-font-size must be positive")
	}
	if cfg.Theme == "" || cfg.Theme == "auto" {
		cfg.Theme = "dark"
	}
	if cfg.CellWidth < 0 || cfg.CellHeight < 0 || cfg.Padding < 0 {
		return errors.New("SVG dimensions cannot be negative")
	}
	if cfg.Theme != "dark" && cfg.Theme != "light" {
		return fmt.Errorf("unsupported theme %q", cfg.Theme)
	}
	return nil
}

func (cfg *Config) applyBackgroundOverride() error {
	if cfg.Background == "" {
		return nil
	}
	bg := parseHexColor(cfg.Background)
	if bg == "" {
		return fmt.Errorf("invalid -bg %q, expected #RRGGBB", cfg.Background)
	}
	cfg.Colors.Background = bg
	return nil
}

type renderStats struct {
	Frames   int
	Duration time.Duration
	Size     int64
}

// newEmulatorScreen builds a terminal emulator screen with terminfo loaded for
// the current TERM (falling back to xterm-256color).
func newEmulatorScreen(cols, rows int) *terminal.Screen {
	screen := terminal.NewScreen(cols, rows)
	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}
	if info, ok := terminal.LoadTerminfo(termName); ok {
		screen.SetTerminfo(info)
	}
	return screen
}

func render(ctx context.Context, cfg Config, logPath string, totalDuration time.Duration) (renderStats, error) {
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

	written := &countingWriter{w: out.file}
	bufferedOut := bufio.NewWriterSize(written, 256*1024)
	// Optionally compress the SVG stream into a .svgz. The renderer writes through
	// the gzip writer, which writes compressed bytes into the buffered file.
	var renderTarget io.Writer = bufferedOut
	var gzipWriter *gzip.Writer
	if cfg.Gzip {
		gzipWriter, err = gzip.NewWriterLevel(bufferedOut, gzip.BestCompression)
		if err != nil {
			return renderStats{}, fmt.Errorf("init gzip writer: %w", err)
		}
		renderTarget = gzipWriter
	}
	renderer := svg.NewRenderer(renderTarget, svg.Config{
		Cols:          cfg.Cols,
		Rows:          cfg.Rows,
		Theme:         cfg.Theme,
		FontSize:      cfg.FontSize,
		FontFamily:    cfg.FontFamily,
		Colors:        cfg.Colors,
		CellWidth:     cfg.CellWidth,
		CellHeight:    cfg.CellHeight,
		Padding:       cfg.Padding,
		Loop:          !cfg.NoLoop,
		TotalDuration: totalDuration,
		EndHold:       cfg.EndHold,
	})
	if err := renderer.Begin(); err != nil {
		return renderStats{}, err
	}

	screen := newEmulatorScreen(cfg.Cols, cfg.Rows)
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
	if gzipWriter != nil {
		if err := gzipWriter.Close(); err != nil {
			return renderStats{}, fmt.Errorf("finish gzip stream: %w", err)
		}
	}
	if err := bufferedOut.Flush(); err != nil {
		return renderStats{}, fmt.Errorf("flush SVG: %w", err)
	}
	if err := out.close(); err != nil {
		return renderStats{}, err
	}
	stats.Size = written.n
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

		if dirty && lastEventAt == 0 && lastSnapshotAt == 0 && record.At > 0 {
			if err := capture(0); err != nil {
				return renderStats{}, err
			}
		}

		if dirty && idleInterval > 0 && record.At-lastEventAt >= idleInterval {
			if err := capture(lastEventAt); err != nil {
				return renderStats{}, err
			}
		}

		// Alternate-screen apps are captured at settled boundaries only (below,
		// interval captures are skipped) to avoid mid-repaint tearing. But an
		// animation whose repaint gaps hover just under the idle interval would
		// then never produce a frame until it exits. Treat a gap of a quarter
		// idle interval as a frame boundary too — bursts inside one repaint are
		// far tighter than that — while frameInterval still caps the rate.
		if dirty && idleInterval > 0 && screen.AlternateActive() &&
			record.At-lastSnapshotAt >= frameInterval &&
			record.At-lastEventAt >= idleInterval/4 {
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
	stdout    bool
	committed bool
}

func createOutput(path string) (*outputFile, error) {
	if path == "-" {
		return &outputFile{file: os.Stdout, path: path, stdout: true}, nil
	}
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
	if f.stdout {
		f.committed = true
		return nil
	}
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("close output file: %w", err)
	}
	if err := os.Rename(f.tempPath, f.path); err != nil {
		return fmt.Errorf("move output into place: %w", err)
	}
	f.committed = true
	return nil
}

func (f *outputFile) cleanup() {
	if f == nil || f.committed || f.stdout {
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

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}
