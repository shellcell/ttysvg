package eventlog

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteOutput(1500*time.Microsecond, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteOutput(2500*time.Microsecond, []byte("world")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r := NewReader(bytes.NewReader(buf.Bytes()))
	record, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if record.At != 1500*time.Microsecond || string(record.Data) != "hello" {
		t.Fatalf("first record = (%s, %q)", record.At, record.Data)
	}

	record, err = r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if record.At != 2500*time.Microsecond || string(record.Data) != "world" {
		t.Fatalf("second record = (%s, %q)", record.At, record.Data)
	}

	_, err = r.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}
