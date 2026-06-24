package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

type progressBar struct {
	out     io.Writer
	total   int64
	enabled bool
	last    time.Time
}

func newProgressBar(total int64, stderr *os.File, quiet bool) *progressBar {
	return &progressBar{
		out:     stderr,
		total:   total,
		enabled: !quiet && stderr != nil && term.IsTerminal(int(stderr.Fd())) && total > 0,
	}
}

func (p *progressBar) Start() {
	if !p.enabled {
		return
	}
	p.last = time.Now()
	fmt.Fprint(p.out, "ttysvg: converting [------------------------] 0%")
}

func (p *progressBar) Update(done int64) {
	if !p.enabled {
		return
	}
	now := time.Now()
	if done < p.total && now.Sub(p.last) < 80*time.Millisecond {
		return
	}
	p.last = now
	if done > p.total {
		done = p.total
	}
	percent := int(done * 100 / p.total)
	filled := percent * 24 / 100
	fmt.Fprintf(p.out, "\rttysvg: converting [%s%s] %d%%",
		strings.Repeat("#", filled), strings.Repeat("-", 24-filled), percent)
}

func (p *progressBar) Finish() {
	if !p.enabled {
		return
	}
	fmt.Fprint(p.out, "\rttysvg: converting [########################] 100%\n")
}
