package app

import "testing"

func TestSvgzPath(t *testing.T) {
	cases := map[string]string{
		"/tmp/out.svg":  "/tmp/out.svgz",
		"/tmp/out.svgz": "/tmp/out.svgz",
		"/tmp/out.SVG":  "/tmp/out.SVGz",
		"/tmp/ttysvg":   "/tmp/ttysvg.svgz",
		"out":           "out.svgz",
	}
	for in, want := range cases {
		if got := svgzPath(in); got != want {
			t.Errorf("svgzPath(%q) = %q, want %q", in, got, want)
		}
	}
}
