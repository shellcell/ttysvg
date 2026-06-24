package ptyrec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type Sink interface {
	WriteOutput(at time.Duration, data []byte) error
}

type Recorder struct {
	Command []string
	Cols    int
	Rows    int
	Stdin   *os.File
	Stdout  io.Writer
	Stderr  io.Writer
}

func (r Recorder) Run(ctx context.Context, sink Sink) (int, error) {
	if len(r.Command) == 0 {
		return 1, errors.New("missing command")
	}
	cmd := exec.CommandContext(ctx, r.Command[0], r.Command[1:]...)
	cmd.Env = environment()

	if r.Stdin != nil && term.IsTerminal(int(r.Stdin.Fd())) {
		state, err := term.MakeRaw(int(r.Stdin.Fd()))
		if err != nil {
			return 1, fmt.Errorf("switch terminal to raw mode: %w", err)
		}
		defer term.Restore(int(r.Stdin.Fd()), state)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(r.Cols), Rows: uint16(r.Rows)})
	if err != nil {
		return 1, fmt.Errorf("start PTY command: %w", err)
	}
	defer ptmx.Close()

	if r.Stdin != nil {
		go func() {
			_, _ = io.Copy(ptmx, r.Stdin)
		}()
	}

	start := time.Now()
	buf := make([]byte, 64*1024)
	var readErr error
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if err := writeFull(r.Stdout, chunk); err != nil {
				readErr = fmt.Errorf("write terminal output: %w", err)
				break
			}
			if err := sink.WriteOutput(time.Since(start), chunk); err != nil {
				readErr = fmt.Errorf("write event log: %w", err)
				break
			}
		}
		if err != nil {
			if !isExpectedPTYClose(err) {
				readErr = fmt.Errorf("read PTY: %w", err)
			}
			break
		}
	}

	waitErr := cmd.Wait()
	if readErr != nil {
		return exitCode(waitErr), readErr
	}
	if waitErr != nil {
		code := exitCode(waitErr)
		if code >= 0 {
			return code, nil
		}
		return 1, waitErr
	}
	return 0, nil
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func isExpectedPTYClose(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func environment() []string {
	env := os.Environ()
	for _, item := range env {
		if strings.HasPrefix(item, "TERM=") {
			return env
		}
	}
	return append(env, "TERM=xterm-256color")
}
