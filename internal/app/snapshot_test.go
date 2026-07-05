package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rabarbra/ttysvg/internal/terminal"
)

func TestTextSnapshotLinesTrimPaddingAndBlankBottom(t *testing.T) {
	frame := terminal.Frame{Cols: 4, Rows: 3, Data: []terminal.Cell{
		{Ch: 'A'}, terminal.BlankCell(), {Ch: 'B'}, terminal.BlankCell(),
		terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell(),
		{Ch: 'C'}, terminal.BlankCell(), terminal.BlankCell(), terminal.BlankCell(),
	}}

	got := textSnapshotLines(frame)
	want := []string{"A B", "", "C"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("textSnapshotLines() = %#v, want %#v", got, want)
	}
}

func TestTextSnapshotLineSkipsHiddenAndWideContinuation(t *testing.T) {
	line := textSnapshotLine([]terminal.Cell{
		{Ch: 'A'},
		{Ch: 'X', Style: terminal.Style{Attrs: terminal.AttrHidden}},
		{Ch: '你', Style: terminal.Style{Attrs: terminal.AttrWide}},
		{Ch: ' ', Style: terminal.Style{Attrs: terminal.AttrWideContinuation}},
		{Ch: 'B'},
	})
	if want := "A 你B"; line != want {
		t.Fatalf("textSnapshotLine() = %q, want %q", line, want)
	}
}

func TestRenderTextSnapshotWritesPlainText(t *testing.T) {
	frame := terminal.Frame{Cols: 3, Rows: 1, Data: []terminal.Cell{{Ch: 'O'}, {Ch: 'K'}, terminal.BlankCell()}}
	path := filepath.Join(t.TempDir(), "snapshot.txt")
	size, err := renderTextSnapshot(path, frame)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "OK\n" {
		t.Fatalf("text snapshot content = %q, want %q", data, "OK\n")
	}
	if size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", size, len(data))
	}
}
