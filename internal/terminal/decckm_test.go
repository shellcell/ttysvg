package terminal

import "testing"

func TestScreenTracksApplicationCursorKeys(t *testing.T) {
	s := NewScreen(10, 2)
	if s.ApplicationCursorKeys() {
		t.Fatal("DECCKM should default to normal mode")
	}
	s.Write([]byte("\x1b[?1h"))
	if !s.ApplicationCursorKeys() {
		t.Fatal("DECCKM should be enabled after ESC [ ? 1 h")
	}
	s.Write([]byte("\x1b[?1l"))
	if s.ApplicationCursorKeys() {
		t.Fatal("DECCKM should be disabled after ESC [ ? 1 l")
	}
	// A full reset must also clear the mode.
	s.Write([]byte("\x1b[?1h"))
	s.Write([]byte("\x1bc"))
	if s.ApplicationCursorKeys() {
		t.Fatal("RIS should clear DECCKM")
	}
}
