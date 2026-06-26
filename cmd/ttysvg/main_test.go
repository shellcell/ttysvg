package main

import "testing"

func TestParseSize(t *testing.T) {
	cases := []struct {
		in         string
		cols, rows int
		wantErr    bool
	}{
		{"", 0, 0, false},
		{"100x30", 100, 30, false},
		{"100x", 100, 0, false},
		{"x30", 0, 30, false},
		{"100X30", 100, 30, false},
		{" 100 x 30 ", 100, 30, false},
		{"100", 0, 0, true},
		{"x", 0, 0, true},
		{"0x0", 0, 0, true},
		{"-5x10", 0, 0, true},
		{"100xfoo", 0, 0, true},
	}
	for _, tc := range cases {
		cols, rows, err := parseSize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSize(%q) = (%d,%d), want error", tc.in, cols, rows)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if cols != tc.cols || rows != tc.rows {
			t.Errorf("parseSize(%q) = (%d,%d), want (%d,%d)", tc.in, cols, rows, tc.cols, tc.rows)
		}
	}
}

func TestFindRetiredFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // "" means none
	}{
		{"cols", []string{"-cols", "80"}, "cols"},
		{"cols-eq", []string{"-cols=80"}, "cols"},
		{"frame-ms", []string{"-o", "x.svg", "-frame-ms", "80"}, "frame-ms"},
		{"clear", []string{"--clear=false"}, "clear"},
		{"value-not-mistaken", []string{"-size", "100x", "-idle", "60ms"}, ""},
		{"after-double-dash", []string{"--", "cmd", "-cols"}, ""},
		{"after-command", []string{"mycmd", "-cols"}, ""},
		{"current-flags-ok", []string{"-size", "x30", "-no-clear", "-frame", "80ms"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, _, ok := findRetiredFlag(tc.args)
			if tc.want == "" {
				if ok {
					t.Fatalf("findRetiredFlag(%v) = %q, want none", tc.args, name)
				}
				return
			}
			if !ok || name != tc.want {
				t.Fatalf("findRetiredFlag(%v) = (%q,%v), want %q", tc.args, name, ok, tc.want)
			}
		})
	}
}
