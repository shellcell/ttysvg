package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Pane control keys, always shown in the status bar so they are discoverable.
// They are intercepted before reaching the child while the pane is active
// (direct-mode recording filters nothing, so apps that need these bytes —
// e.g. telnet's Ctrl-] escape — still receive them there). These two are the
// least-contended control bytes: Ctrl-\ only means SIGQUIT (and is asciinema's
// pause key), and Ctrl-] is telnet's own "escape the session" key; neither is
// used by readline, tmux, screen, or fzf.
const (
	keyToggle = 0x1c // Ctrl-\ : start / pause / resume
	keyStop   = 0x1d // Ctrl-] : stop and save
	frameHold = 250 * time.Millisecond
)

type recordingState uint8

const (
	recordingPreparing recordingState = iota
	recordingActive
	recordingPaused
	recordingStopped
)

type recordingControl struct {
	mu            sync.Mutex
	sink          eventSink
	live          *liveTerminal
	cancel        context.CancelFunc
	state         recordingState
	started       bool
	stopRequested bool
	base          time.Duration
	activeStart   time.Time
	err           error
	stopTicks     chan struct{}
	tickOnce      sync.Once
}

type eventSink interface {
	WriteOutput(at time.Duration, data []byte) error
}

func newRecordingControl(sink eventSink, live *liveTerminal, cancel context.CancelFunc) *recordingControl {
	return &recordingControl{sink: sink, live: live, cancel: cancel, state: recordingPreparing, stopTicks: make(chan struct{})}
}

func (c *recordingControl) WriteOutput(_ time.Duration, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if c.state != recordingActive {
		return nil
	}
	at := c.base + time.Since(c.activeStart)
	if err := c.sink.WriteOutput(at, data); err != nil {
		c.err = err
		return err
	}
	return nil
}

func (c *recordingControl) StartOrResume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case recordingPreparing:
		c.startLocked()
	case recordingPaused:
		c.resumeLocked()
	}
}

func (c *recordingControl) PauseOrResume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case recordingActive:
		c.pauseLocked()
	case recordingPaused:
		c.resumeLocked()
	case recordingPreparing:
		c.startLocked()
	}
}

func (c *recordingControl) Stop() {
	c.mu.Lock()
	if c.state == recordingActive {
		c.base += time.Since(c.activeStart)
	}
	c.state = recordingStopped
	c.stopRequested = true
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *recordingControl) Started() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started
}

func (c *recordingControl) StopRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopRequested
}

func (c *recordingControl) State() recordingState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *recordingControl) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *recordingControl) Close() {
	close(c.stopTicks)
}

func (c *recordingControl) startLocked() {
	if c.err != nil || c.state == recordingStopped {
		return
	}
	c.started = true
	c.writePrefixLocked(0)
	c.base = frameHold
	c.activeStart = time.Now()
	c.state = recordingActive
	c.updateLiveLocked()
	c.startTickerLocked()
}

func (c *recordingControl) pauseLocked() {
	c.base += time.Since(c.activeStart)
	c.state = recordingPaused
	c.updateLiveLocked()
}

func (c *recordingControl) resumeLocked() {
	if c.err != nil || c.state == recordingStopped {
		return
	}
	c.writePrefixLocked(c.base)
	c.base += frameHold
	c.activeStart = time.Now()
	c.state = recordingActive
	c.updateLiveLocked()
}

func (c *recordingControl) writePrefixLocked(at time.Duration) {
	if c.live == nil {
		return
	}
	prefix := c.live.RecordingPrefix()
	if len(prefix) == 0 {
		return
	}
	if err := c.sink.WriteOutput(at, prefix); err != nil {
		c.err = fmt.Errorf("write initial event log frame: %w", err)
	}
}

func (c *recordingControl) startTickerLocked() {
	c.tickOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.tick()
				case <-c.stopTicks:
					return
				}
			}
		}()
	})
}

func (c *recordingControl) tick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateLiveLocked()
}

func (c *recordingControl) updateLiveLocked() {
	if c.live == nil {
		return
	}
	elapsed := c.base
	if c.state == recordingActive {
		elapsed += time.Since(c.activeStart)
	}
	c.live.UpdateRecordingState(c.state, elapsed)
}

type paneInputReader struct {
	src     *os.File
	live    *liveTerminal
	control *recordingControl
	buf     []byte
}

func newPaneInputReader(src *os.File, live *liveTerminal, control *recordingControl) io.Reader {
	return &paneInputReader{src: src, live: live, control: control}
}

// SetReadDeadline lets the recorder unblock the stdin copier at shutdown.
func (r *paneInputReader) SetReadDeadline(t time.Time) error {
	return r.src.SetReadDeadline(t)
}

func (r *paneInputReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		var tmp [4096]byte
		n, err := r.src.Read(tmp[:])
		if n > 0 {
			r.buf = r.live.FilterInput(tmp[:n], r.control)
		}
		if err != nil {
			if len(r.buf) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}
