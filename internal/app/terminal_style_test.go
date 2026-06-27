package app

import "testing"

func TestParseWezTermConfig(t *testing.T) {
	cfg := `local wezterm = require 'wezterm'
return {
  font_size = 13.5,
  font = wezterm.font_with_fallback({ "JetBrains Mono", "Fira Code" }),
  color_scheme = "Builtin Dark",
  colors = {
    foreground = "#abcdef",
    background = '#102030',
    ansi = { "#000000", "#111111", "#222222", "#333333", "#444444", "#555555", "#666666", "#777777" },
    brights = { "#888888", "#999999", "#aaaaaa", "#bbbbbb", "#cccccc", "#dddddd", "#eeeeee", "#ffffff" },
  },
}`
	style := parseWezTermConfig(cfg)
	if style.FontSize != 13.5 {
		t.Errorf("FontSize = %v, want 13.5", style.FontSize)
	}
	if style.FontFamily != "JetBrains Mono, Fira Code" {
		t.Errorf("FontFamily = %q, want %q", style.FontFamily, "JetBrains Mono, Fira Code")
	}
	if style.Colors.Foreground != "#abcdef" {
		t.Errorf("Foreground = %q, want #abcdef", style.Colors.Foreground)
	}
	if style.Colors.Background != "#102030" {
		t.Errorf("Background = %q, want #102030", style.Colors.Background)
	}
	if style.Colors.ANSI[0] != "#000000" || style.Colors.ANSI[7] != "#777777" {
		t.Errorf("ansi range = %q..%q", style.Colors.ANSI[0], style.Colors.ANSI[7])
	}
	if style.Colors.ANSI[8] != "#888888" || style.Colors.ANSI[15] != "#ffffff" {
		t.Errorf("brights range = %q..%q", style.Colors.ANSI[8], style.Colors.ANSI[15])
	}
}

func TestQuotedStrings(t *testing.T) {
	got := quotedStrings(`{ "a", 'b' , "c" }`)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("quotedStrings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("quotedStrings = %v, want %v", got, want)
		}
	}
}

func TestJSONCToJSON(t *testing.T) {
	in := `{
  // line comment
  "a": 1, /* block */
  "list": [1, 2, 3,],
  "obj": { "x": 1, },
}`
	out := jsoncToJSON(in)
	for _, bad := range []string{",]", ", ]", ",}", ", }", "//", "/*"} {
		if contains(out, bad) {
			t.Fatalf("jsoncToJSON left %q in output:\n%s", bad, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
