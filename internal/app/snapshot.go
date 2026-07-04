package app

import (
	"bufio"
	"fmt"

	"github.com/rabarbra/ttysvg/internal/svg"
	"github.com/rabarbra/ttysvg/internal/terminal"
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
