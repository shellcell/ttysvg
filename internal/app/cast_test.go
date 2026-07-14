package app

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shellcell/ttysvg/internal/eventlog"
)

func readAllRecords(t *testing.T, buf *bytes.Buffer) []eventlog.Record {
	t.Helper()
	reader := eventlog.NewReader(bytes.NewReader(buf.Bytes()))
	var records []eventlog.Record
	for {
		record, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), record.Data...)
		records = append(records, eventlog.Record{At: record.At, Data: data})
	}
	return records
}

func TestConvertCastV2(t *testing.T) {
	cast := `{"version": 2, "width": 100, "height": 30, "idle_time_limit": 1}
[0.1, "o", "hello"]
[0.25, "i", "typed"]
[5.0, "o", "after long pause"]
`
	var buf bytes.Buffer
	writer := eventlog.NewWriter(&buf)
	cols, rows, err := convertCast(strings.NewReader(cast), writer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if cols != 100 || rows != 30 {
		t.Fatalf("size = %dx%d, want 100x30", cols, rows)
	}
	records := readAllRecords(t, &buf)
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (input events skipped)", len(records))
	}
	if string(records[0].Data) != "hello" || records[0].At != 100*time.Millisecond {
		t.Fatalf("record 0 = %q at %s", records[0].Data, records[0].At)
	}
	// The 4.75s silent gap is capped at idle_time_limit=1s: 0.25s + 1s = 1.25s.
	if string(records[1].Data) != "after long pause" || records[1].At != 1250*time.Millisecond {
		t.Fatalf("record 1 = %q at %s", records[1].Data, records[1].At)
	}
}

func TestConvertCastV3Intervals(t *testing.T) {
	cast := `{"version": 3, "term": {"cols": 80, "rows": 24}}
[0.5, "o", "a"]
[0.5, "o", "b"]
[0.25, "r", "120x40"]
[0.25, "o", "c"]
`
	var buf bytes.Buffer
	writer := eventlog.NewWriter(&buf)
	var warnings strings.Builder
	cols, rows, err := convertCast(strings.NewReader(cast), writer, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if cols != 80 || rows != 24 {
		t.Fatalf("size = %dx%d, want 80x24", cols, rows)
	}
	records := readAllRecords(t, &buf)
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	// v3 timestamps are intervals: cumulative 0.5, 1.0, 1.5.
	wantAt := []time.Duration{500 * time.Millisecond, time.Second, 1500 * time.Millisecond}
	for i, want := range wantAt {
		if records[i].At != want {
			t.Fatalf("record %d at %s, want %s", i, records[i].At, want)
		}
	}
	if !strings.Contains(warnings.String(), "resizes") {
		t.Fatalf("expected resize warning, got %q", warnings.String())
	}
}

func TestConvertCastRejectsUnknownVersion(t *testing.T) {
	var buf bytes.Buffer
	writer := eventlog.NewWriter(&buf)
	_, _, err := convertCast(strings.NewReader(`{"version": 1, "width": 80, "height": 24}`), writer, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported cast version") {
		t.Fatalf("err = %v, want unsupported version", err)
	}
}
