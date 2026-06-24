package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rabarbra/ttysvg/internal/eventlog"
	"github.com/rabarbra/ttysvg/internal/terminal"
)

func TestReplayAvoidsIntervalCaptureInAlternateScreen(t *testing.T) {
	var buf bytes.Buffer
	writer := eventlog.NewWriter(&buf)
	mustWriteOutput(t, writer, 0, []byte("\x1b[?1049hABCDEF"))
	mustWriteOutput(t, writer, 200*time.Millisecond, []byte("\rX"))
	mustWriteOutput(t, writer, 201*time.Millisecond, []byte("YZ"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	frames := &recordingFrameWriter{}
	_, err := replay(
		context.Background(),
		eventlog.NewReader(bytes.NewReader(buf.Bytes())),
		terminal.NewScreen(6, 1),
		frames,
		80*time.Millisecond,
		60*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, frame := range frames.rows {
		if frame == "XBCDEF" {
			t.Fatalf("captured mid-repaint alternate-screen frame %q; frames=%q", frame, frames.rows)
		}
	}
	if len(frames.rows) == 0 {
		t.Fatal("no frames captured")
	}
	if got := frames.rows[len(frames.rows)-1]; got != "XYZDEF" {
		t.Fatalf("final frame = %q, want %q; frames=%q", got, "XYZDEF", frames.rows)
	}
}

func mustWriteOutput(t *testing.T, writer *eventlog.Writer, at time.Duration, data []byte) {
	t.Helper()
	if err := writer.WriteOutput(at, data); err != nil {
		t.Fatal(err)
	}
}

type recordingFrameWriter struct {
	rows []string
}

func (w *recordingFrameWriter) WriteFrame(frame terminal.Frame, _ time.Duration, _ time.Duration) error {
	w.rows = append(w.rows, frameRowString(frame, 0))
	return nil
}

func (w *recordingFrameWriter) WriteFinalFrame(frame terminal.Frame, _ time.Duration) error {
	w.rows = append(w.rows, frameRowString(frame, 0))
	return nil
}

func (w *recordingFrameWriter) FrameCount() int {
	return len(w.rows)
}

func frameRowString(frame terminal.Frame, row int) string {
	cells := frame.Row(row)
	runes := make([]rune, len(cells))
	for i, cell := range cells {
		runes[i] = cell.Rune()
	}
	return string(runes)
}
