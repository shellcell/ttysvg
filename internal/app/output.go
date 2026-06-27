package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const defaultOutputName = "ttysvg.svg"

// svgzPath normalizes a resolved output path to the .svgz extension used for
// gzip-compressed output: a .svg becomes .svgz, an existing .svgz is kept, and
// anything else gets .svgz appended.
func svgzPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".svgz"):
		return path
	case strings.HasSuffix(lower, ".svg"):
		return path + "z"
	default:
		return path + ".svgz"
	}
}

func prepareOutputPath(request string, stdin *os.File, stderr io.Writer) (string, error) {
	path, err := resolveOutputPath(request)
	if err == nil {
		err = ensureWritableTarget(path)
	}
	if err == nil {
		return path, nil
	}

	if stdin == nil || !term.IsTerminal(int(stdin.Fd())) {
		return "", fmt.Errorf("output location is not writable: %w", err)
	}
	return promptOutputPath(path, err, stdin, stderr)
}

func resolveOutputPath(request string) (string, error) {
	if request == "" {
		request = "."
	}
	if request == "-" {
		return "", fmt.Errorf("-o - is not supported while recording an interactive terminal")
	}

	abs := request
	if !filepath.IsAbs(abs) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)

	if outputIsDirectoryTarget(request, abs) {
		abs = filepath.Join(abs, defaultOutputName)
	}
	return abs, nil
}

func outputIsDirectoryTarget(raw string, abs string) bool {
	if raw == "" || raw == "." {
		return true
	}
	if st, err := os.Stat(abs); err == nil {
		return st.IsDir()
	}
	if strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, "\\") {
		return true
	}
	if filepath.Ext(filepath.Base(raw)) == "" {
		return true
	}
	return false
}

func ensureWritableTarget(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", dir, err)
	}
	return nil
}

func promptOutputPath(failedPath string, cause error, stdin *os.File, stderr io.Writer) (string, error) {
	now := time.Now().Format("20060102-150405")
	options := []string{
		filepath.Join(os.TempDir(), defaultOutputName),
		filepath.Join(os.TempDir(), "ttysvg-"+now+".svg"),
		filepath.Join(os.TempDir(), "ttysvg", defaultOutputName),
	}

	fmt.Fprintf(stderr, "ttysvg: cannot write to %s: %v\n", failedPath, cause)
	fmt.Fprintln(stderr, "Choose an output path before recording starts:")
	for i, option := range options {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, option)
	}
	fmt.Fprint(stderr, "Enter 1-3 or a custom path: ")

	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read output path: %w", err)
		}
		return "", fmt.Errorf("no output path selected")
	}
	answer := strings.TrimSpace(scanner.Text())
	if answer == "" {
		return "", fmt.Errorf("no output path selected")
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(options) {
		answer = options[n-1]
	}

	path, err := resolveOutputPath(answer)
	if err != nil {
		return "", err
	}
	if err := ensureWritableTarget(path); err != nil {
		return "", fmt.Errorf("selected output location is not writable: %w", err)
	}
	return path, nil
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
