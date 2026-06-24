package app

import "testing"

func TestParseTerminalStyleResponse(t *testing.T) {
	style := parseTerminalStyleResponse("\x1b]10;rgb:ffff/eeee/dddd\x1b\\\x1b]11;rgb:0011/2233/4455\x1b\\\x1b]4;1;rgb:aa00/bb00/cc00\x1b\\")

	if style.Colors.Foreground != "#ffeedd" {
		t.Fatalf("foreground = %q", style.Colors.Foreground)
	}
	if style.Colors.Background != "#002244" {
		t.Fatalf("background = %q", style.Colors.Background)
	}
	if style.Colors.ANSI[1] != "#a9bacb" {
		t.Fatalf("ansi red = %q", style.Colors.ANSI[1])
	}
	if style.Theme != "dark" {
		t.Fatalf("theme = %q", style.Theme)
	}
}
