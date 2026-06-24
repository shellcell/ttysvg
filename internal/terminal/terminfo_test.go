package terminal

import "testing"

func TestParseTerminfoFixedCapabilities(t *testing.T) {
	info, ok := ParseTerminfo([]byte(`x|test,
	clear=\E[H\E[2J,
	civis=\E[?25l,
	cnorm=\E[?25h,
`))
	if !ok {
		t.Fatal("terminfo was not parsed")
	}

	s := NewScreen(4, 2)
	s.SetTerminfo(info)
	s.Write([]byte("abcd"))
	s.Write([]byte("\x1b[H\x1b[2J"))
	frame := s.Snapshot()
	defer frame.Release()
	if got := stringRunes(frame.Row(0)); got != "    " {
		t.Fatalf("row after clear = %q", got)
	}

	s.Write([]byte("\x1b[?25l"))
	frame = s.Snapshot()
	defer frame.Release()
	if frame.CursorVisible {
		t.Fatal("cursor should be hidden")
	}
}

func TestTerminfoCursorAddress(t *testing.T) {
	info, ok := ParseTerminfo([]byte(`x|test,
	cup=\E[%i%p1%d;%p2%dH,
`))
	if !ok {
		t.Fatal("terminfo was not parsed")
	}

	s := NewScreen(5, 3)
	s.SetTerminfo(info)
	s.Write([]byte("\x1b[2;4HX"))
	frame := s.Snapshot()
	defer frame.Release()

	if got := frame.Row(1)[3].Rune(); got != 'X' {
		t.Fatalf("cell at cursor address = %q", got)
	}
}
