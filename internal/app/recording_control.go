package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	keyToggle            = 0x1c // Ctrl-\ : start / pause / resume; double press snapshots in pane mode
	keyStop              = 0x1d // Ctrl-] : stop and save
	frameHold            = 250 * time.Millisecond
	keyDoublePressWindow = 250 * time.Millisecond
	snapshotMessageHold  = 2 * time.Second
)

type recordingState uint8

const (
	recordingPreparing recordingState = iota
	recordingActive
	recordingPaused
	recordingStopped
)

type recordingControl struct {
	mu             sync.Mutex
	sink           eventSink
	live           *liveTerminal
	cancel         context.CancelFunc
	state          recordingState
	started        bool
	stopRequested  bool
	base           time.Duration
	activeStart    time.Time
	err            error
	stopTicks      chan struct{}
	tickOnce       sync.Once
	toggleTimer    *time.Timer
	togglePending  bool
	messageTimer   *time.Timer
	messageToken   uint64
	messageVisible bool
	snapshots      []string
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

func (c *recordingControl) ToggleKey() {
	var snapshot bool
	c.mu.Lock()
	if c.err != nil || c.state == recordingStopped {
		c.mu.Unlock()
		return
	}
	if c.togglePending {
		c.clearPendingToggleLocked()
		snapshot = true
	} else {
		c.armToggleLocked()
	}
	c.mu.Unlock()
	if snapshot {
		c.saveSnapshot()
	}
}

func (c *recordingControl) StartOrResume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearPendingToggleLocked()
	c.clearTemporaryMessageLocked()
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
	c.clearPendingToggleLocked()
	c.clearTemporaryMessageLocked()
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
	c.clearPendingToggleLocked()
	c.clearTemporaryMessageLocked()
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
	c.mu.Lock()
	c.clearPendingToggleLocked()
	c.clearTemporaryMessageLocked()
	c.mu.Unlock()
	close(c.stopTicks)
}

func (c *recordingControl) Snapshots() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.snapshots...)
}

func (c *recordingControl) armToggleLocked() {
	c.clearPendingToggleLocked()
	c.togglePending = true
	c.toggleTimer = time.AfterFunc(keyDoublePressWindow, c.fireToggle)
}

func (c *recordingControl) clearPendingToggleLocked() {
	c.togglePending = false
	if c.toggleTimer != nil {
		c.toggleTimer.Stop()
		c.toggleTimer = nil
	}
}

func (c *recordingControl) fireToggle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.togglePending || c.err != nil || c.state == recordingStopped {
		return
	}
	c.togglePending = false
	c.toggleTimer = nil
	c.clearTemporaryMessageLocked()
	switch c.state {
	case recordingPreparing:
		c.startLocked()
	case recordingActive:
		c.pauseLocked()
	case recordingPaused:
		c.resumeLocked()
	}
}

func (c *recordingControl) saveSnapshot() {
	if c.live == nil {
		return
	}
	frame, ok, err := c.live.SnapshotFrame()
	if err == nil && !ok {
		err = fmt.Errorf("snapshot is available only in pane mode")
	}
	svgPath := ""
	textPath := ""
	if err == nil {
		now := time.Now()
		svgPath, err = resolveSnapshotOutputPath(c.live.cfg.OutputPath, now)
		if err == nil {
			textPath, err = resolveTextSnapshotOutputPath(c.live.cfg.OutputPath, now)
		}
	}
	if err == nil {
		_, err = renderSnapshot(svgPath, c.live.cfg, frame)
	}
	if err == nil {
		_, err = renderTextSnapshot(textPath, frame)
	}
	frame.Release()
	if err != nil {
		c.showTemporaryMessage("snapshot failed: "+err.Error(), snapshotMessageHold)
		return
	}
	c.mu.Lock()
	c.snapshots = append(c.snapshots, svgPath, textPath)
	c.mu.Unlock()
	c.showTemporaryMessage("snapshot saved: "+filepath.Base(svgPath)+" + "+filepath.Base(textPath), snapshotMessageHold)
}

func (c *recordingControl) showTemporaryMessage(message string, hold time.Duration) {
	if c.live == nil {
		return
	}
	c.mu.Lock()
	c.clearTemporaryMessageLocked()
	token := c.messageToken
	c.messageVisible = true
	c.messageTimer = time.AfterFunc(hold, func() { c.restoreTemporaryMessage(token) })
	c.mu.Unlock()
	c.live.ShowControlMessage(message)
}

func (c *recordingControl) restoreTemporaryMessage(token uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token != c.messageToken || c.state == recordingStopped {
		return
	}
	c.messageVisible = false
	c.messageTimer = nil
	c.updateLiveLocked()
}

func (c *recordingControl) clearTemporaryMessageLocked() {
	c.messageToken++
	c.messageVisible = false
	if c.messageTimer != nil {
		c.messageTimer.Stop()
		c.messageTimer = nil
	}
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
	if c.messageVisible {
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
