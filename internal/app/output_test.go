package app

import (
	"path/filepath"
	"testing"
	"time"
)

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

func TestTimestampedOutputName(t *testing.T) {
	at := time.Date(2026, 7, 4, 15, 6, 7, 0, time.UTC)
	if got, want := timestampedOutputName(animationOutputPrefix, at), "ttyanim_2026.07.04-15.06.07.svg"; got != want {
		t.Fatalf("animation name = %q, want %q", got, want)
	}
	if got, want := timestampedOutputName(snapshotOutputPrefix, at), "ttypic_2026.07.04-15.06.07.svg"; got != want {
		t.Fatalf("snapshot name = %q, want %q", got, want)
	}
	if got, want := timestampedTextSnapshotName(at), "ttytxt-2026.07.04-15.06.07.txt"; got != want {
		t.Fatalf("text snapshot name = %q, want %q", got, want)
	}
}

func TestResolveOutputPathDirectoryUsesTimestampedAnimationName(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveOutputPathWithName(dir, "ttyanim_2026.07.04-15.06.07.svg")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "ttyanim_2026.07.04-15.06.07.svg"); got != want {
		t.Fatalf("directory output = %q, want %q", got, want)
	}
}

func TestResolveSnapshotOutputPathUsesAnimationDirectory(t *testing.T) {
	at := time.Date(2026, 7, 4, 15, 6, 7, 0, time.UTC)
	dir := t.TempDir()
	got, err := resolveSnapshotOutputPath(filepath.Join(dir, "demo.svg"), at)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "ttypic_2026.07.04-15.06.07.svg"); got != want {
		t.Fatalf("snapshot output = %q, want %q", got, want)
	}
	got, err = resolveTextSnapshotOutputPath(filepath.Join(dir, "demo.svg"), at)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "ttytxt-2026.07.04-15.06.07.txt"); got != want {
		t.Fatalf("text snapshot output = %q, want %q", got, want)
	}
}
