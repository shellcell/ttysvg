package eventlog

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	maxRecordBytes = 16 << 20
)

var magic = []byte("TTSVGLOG1\n")

type Writer struct {
	w       *bufio.Writer
	scratch [binary.MaxVarintLen64]byte
	closed  bool
}

func NewWriter(w io.Writer) *Writer {
	writer := &Writer{
		w: bufio.NewWriterSize(w, 256*1024),
	}
	_, _ = writer.w.Write(magic)
	return writer
}

func (w *Writer) WriteOutput(at time.Duration, data []byte) error {
	if w.closed {
		return errors.New("event log writer is closed")
	}
	if len(data) == 0 {
		return nil
	}
	if len(data) > maxRecordBytes {
		return fmt.Errorf("event record too large: %d bytes", len(data))
	}
	if err := w.writeUvarint(uint64(at / time.Microsecond)); err != nil {
		return err
	}
	if err := w.writeUvarint(uint64(len(data))); err != nil {
		return err
	}
	_, err := w.w.Write(data)
	return err
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.w.Flush()
}

func (w *Writer) writeUvarint(v uint64) error {
	n := binary.PutUvarint(w.scratch[:], v)
	_, err := w.w.Write(w.scratch[:n])
	return err
}

type Record struct {
	At   time.Duration
	Data []byte
}

type Reader struct {
	r      *bufio.Reader
	header bool
	buf    []byte
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 256*1024)}
}

func (r *Reader) Next() (Record, error) {
	if !r.header {
		if err := r.readHeader(); err != nil {
			return Record{}, err
		}
	}
	atMicros, err := binary.ReadUvarint(r.r)
	if err != nil {
		return Record{}, err
	}
	length, err := binary.ReadUvarint(r.r)
	if err != nil {
		return Record{}, err
	}
	if length > maxRecordBytes {
		return Record{}, fmt.Errorf("event record too large: %d bytes", length)
	}
	if cap(r.buf) < int(length) {
		r.buf = make([]byte, int(length))
	}
	data := r.buf[:int(length)]
	if _, err := io.ReadFull(r.r, data); err != nil {
		return Record{}, err
	}
	return Record{At: time.Duration(atMicros) * time.Microsecond, Data: data}, nil
}

func (r *Reader) readHeader() error {
	buf := make([]byte, len(magic))
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return err
	}
	for i := range magic {
		if buf[i] != magic[i] {
			return errors.New("invalid event log header")
		}
	}
	r.header = true
	return nil
}
