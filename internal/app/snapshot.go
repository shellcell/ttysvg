package app

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/shellcell/ttysvg/internal/svg"
	"github.com/shellcell/ttysvg/internal/terminal"
)

func renderSnapshot(path string, cfg Config, frame terminal.Frame) (renderStats, error) {
	out, err := createOutput(path)
	if err != nil {
		return renderStats{}, err
	}
	defer out.cleanup()

	written := &countingWriter{w: out.file}
	bufferedOut := bufio.NewWriterSize(written, 256*1024)
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
		Static:     true,
	})
	if err := renderer.Begin(); err != nil {
		return renderStats{}, err
	}
	if err := renderer.WriteStaticFrame(frame); err != nil {
		return renderStats{}, err
	}
	if err := renderer.End(); err != nil {
		return renderStats{}, err
	}
	if err := bufferedOut.Flush(); err != nil {
		return renderStats{}, fmt.Errorf("flush snapshot SVG: %w", err)
	}
	if err := out.close(); err != nil {
		return renderStats{}, err
	}
	return renderStats{Frames: renderer.FrameCount(), Size: written.n}, nil
}

func renderTextSnapshot(path string, frame terminal.Frame) (int64, error) {
	out, err := createOutput(path)
	if err != nil {
		return 0, err
	}
	defer out.cleanup()

	written := &countingWriter{w: out.file}
	bufferedOut := bufio.NewWriterSize(written, 64*1024)
	for _, line := range textSnapshotLines(frame) {
		if _, err := bufferedOut.WriteString(line + "\n"); err != nil {
			return 0, fmt.Errorf("write text snapshot: %w", err)
		}
	}
	if err := bufferedOut.Flush(); err != nil {
		return 0, fmt.Errorf("flush text snapshot: %w", err)
	}
	if err := out.close(); err != nil {
		return 0, err
	}
	return written.n, nil
}

func textSnapshotLines(frame terminal.Frame) []string {
	lines := make([]string, frame.Rows)
	lastContent := -1
	for row := 0; row < frame.Rows; row++ {
		line := textSnapshotLine(frame.Row(row))
		lines[row] = line
		if line != "" {
			lastContent = row
		}
	}
	if lastContent < 0 {
		return nil
	}
	return lines[:lastContent+1]
}

func textSnapshotLine(cells []terminal.Cell) string {
	var b strings.Builder
	b.Grow(len(cells))
	for _, cell := range cells {
		if cell.WideContinuation() {
			continue
		}
		if cell.Style.Has(terminal.AttrHidden) {
			b.WriteByte(' ')
			continue
		}
		r := cell.Rune()
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
		if cell.Combining != "" {
			b.WriteString(cell.Combining)
		}
	}
	return strings.TrimRight(b.String(), " ")
}
