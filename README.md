# ttysvg

`ttysvg` records an interactive terminal session and converts it to an animated SVG after the session exits. Single static Go binary.

![ttysvg usage animation](/resources/ttysvg.svg)

Output is declarative SVG animation (SMIL + CSS), no scripts, so it renders as a plain image in contexts that strip interactive SVG, including GitHub READMEs. Markup uses per-color CSS classes, row diffing, and trimmed numeric precision; it gzips to well under 10% of raw size.

## Install

```sh
go install github.com/shellcell/ttysvg/cmd/ttysvg@latest   # from source (Go 1.26+)
brew install shellcell/tap/ttysvg                         # Homebrew
nix run github:shellcell/ttysvg                            # Nix flake
```

Prebuilt Linux/macOS binaries and `.deb`/`.rpm` packages are attached to each [GitHub release](https://github.com/shellcell/ttysvg/releases).

## Usage

```sh
ttysvg -o out.svg                     # record $SHELL (fallback /bin/sh); `exit` stops
ttysvg -o demo.svg -- go test ./...   # record one command
ttysvg -o - -- make test > out.svg    # stream to stdout; session moves to stderr
ttysvg -cast demo.cast -o demo.svg    # convert an asciinema v2/v3 cast, no recording
```

On completion `ttysvg` prints the absolute SVG path, file size, frame count, and duration.

**Output paths.** `-o` resolves to a file if the path has an extension (`out.svg`), otherwise to a directory (`./recordings`, existing dirs, and the default `.`). Directory targets are named `ttyanim_<timestamp>.svg`; pane snapshots write `ttypic_<timestamp>.svg` plus a plain-text `ttytxt-<timestamp>.txt`. Writability is checked before the PTY is spawned; on failure with an interactive stdin, `ttysvg` prompts for another path and offers `/tmp` locations.

**`.svgz`.** `-gz`, or an `.svgz` output path, writes the gzip-compressed form — good for self-hosted pages and CI artifacts, where you control `Content-Encoding`. Not for GitHub READMEs: GitHub serves `.svgz` without `Content-Encoding: gzip`, so the browser gets raw deflate and the image fails. Commit plain `.svg`; GitHub's CDN gzips it in transit.

**Casts.** With `-cast`, size comes from the cast header unless `-size` overrides it, and the header's `idle_time_limit` is respected.

## Frame rate

Default 40 fps. Frames are emitted only on screen change, so the rate is a ceiling during output and idle stretches cost nothing. `-fps` sets both capture intervals to `1/fps` and is mutually exclusive with `-frame` (minimum interval between frames) and `-idle` (capture a settled frame after this much output silence; `0` disables), which take Go durations (`33ms`, `1500us`).

Raw size scales roughly linearly with frame rate; gzipped size grows far more slowly, since higher rates mostly add near-duplicate markup. Serving gzip-encoded keeps 60 fps recordings small.

## Fixed-size recording and pane mode

`-size COLSxROWS` pins recording dimensions; omitting either side (`100x`, `x30`) auto-fits that axis. Against the current terminal, a requested size that is **equal** records directly, **larger** refuses to start and requests a resize, and **smaller** starts the child in a pane so the screen can be prepared first.

Pane mode is live and interactive but captures nothing until started. Controls appear in the status bar; mouse buttons are equivalent.

| Key | Action |
| --- | --- |
| `Ctrl-\` | Start / pause / resume. |
| `Ctrl-\\` | Snapshot: static SVG + plain text (any non-stopped state). |
| `Ctrl-]` | Stop. |

- The prepared screen and each resume screen are captured as static frames; the animation continues from those states. Paused output is live but unrecorded.
- The text snapshot is trimmed plain terminal text, suitable for a Markdown ```` ```ascii ```` fence.
- `-padding` is previewed inside the pane border, in whole cells approximated from the SVG cell size.
- `-autostart` skips the initial `Ctrl-\` wait; `-headless` skips the pane entirely and records the requested size directly, even on an interactive terminal.
- Key choice: `Ctrl-\` is otherwise only SIGQUIT (and asciinema's pause key), `Ctrl-]` is telnet's escape; neither is bound by shells, readline, tmux, or fzf. Both are intercepted only while the pane is active — direct mode filters nothing, so recording telnet itself works there.

## Flags

```text
Output
  -o path            output SVG file or directory ("-" for stdout)
  -gz                write a gzip-compressed .svgz file (also implied by a .svgz path)
  -q                 suppress progress and summary

Recording
  -size COLSxROWS    recording size; omit either side to auto-fit (100x, x30, 100x30)
  -fps n             target frames per second; overrides -frame/-idle
  -frame dur         minimum time between SVG snapshots; default 25ms (40fps)
  -idle dur          capture after output silence; default 25ms; 0 disables
  -bg color          terminal background while recording, e.g. #0d1117; also the SVG background
  -query-terminal    query the terminal for colors, theme, and font; off by default
  -no-clear          do not clear the terminal before recording
  -autostart         in pane mode, record immediately instead of waiting for Ctrl-\
  -headless          record the requested size with no pane; for scripting and CI

Appearance
  -theme name        auto, dark, or light; default auto
  -font-family s     SVG CSS font-family; defaults to monospace fallbacks
  -font-size px      SVG font size; does not change the live terminal font; default 14
  -cell-width px     SVG cell width; default font-size*0.62
  -cell-height px    SVG cell height; default font-size*1.25
  -padding px        SVG background frame around the grid; default 0
  -no-loop           play once and freeze the final screen instead of looping
  -hold dur          how long the final screen is held before the loop repeats; default 2s

Other
  -cast path         convert an asciinema .cast file (v2/v3) instead of recording
  -version           print version and exit
```

Terminal identification is off by default to keep startup fast. `-query-terminal` detects colors, theme, and font via OSC queries (10/11 foreground and background, 4 for the ANSI palette, 50 for the font where supported) and returns as soon as the terminal answers, usually within milliseconds; the results supply the defaults for `-font-family` and `-font-size`.

## Continuous integration

With a non-TTY stdout, `ttysvg` records directly — no pane, no `Ctrl-\` — and passes child output to the job log. A command after `--` is required (with no command and a non-interactive stdin it errors rather than launching a shell), and `-size` should be set, since with no terminal to measure it falls back to 80x24. Add `-headless` if the runner allocates a TTY but a fixed-size, non-interactive recording is still wanted.

```yaml
# GitHub Actions
- uses: actions/setup-go@v5
  with: { go-version-file: go.mod }
- run: go install github.com/shellcell/ttysvg/cmd/ttysvg@latest
- run: ttysvg -q -size 100x30 -o out.svg -- make test
- uses: actions/upload-artifact@v4
  with: { name: terminal-recording, path: out.svg }
```

Elsewhere (GitLab CI and other runners), use the published image `ghcr.io/shellcell/ttysvg:latest` and the same `ttysvg -q -size 100x30 -o out.svg -- make test` invocation.

## How it works

Direct mode performs no terminal emulation or SVG work while the child runs:

```text
record:  PTY read -> stdout write -> buffered event-log write
exit:    event log -> ANSI replay -> sampled frames -> streaming row-diff SVG
```

The event log is timestamp varints plus raw PTY byte chunks, so record-time memory is bounded by fixed I/O buffers, independent of session length. The renderer holds no snapshot history — it keeps the current grid plus active row states and emits a row interval when that row changes — so memory is proportional to terminal size, not recording length.

Pane mode live-renders the child PTY through ttysvg's terminal emulator to maintain the pane, controls, pause/resume, and mouse translation. Memory is still bounded by terminal size; CPU cost is higher.

## Comparison

`ttysvg` records a live, interactive session and writes a script-free animated SVG from a single static binary. Related tools trade off differently:

| Tool | Output | Notes |
| --- | --- | --- |
| [termtosvg](https://github.com/nbedos/termtosvg) | Animated SVG | Python; the closest analog |
| [asciinema](https://asciinema.org/) | `.cast` JSON | De-facto standard; needs a player or a converter ([agg](https://github.com/asciinema/agg), [svg-term](https://github.com/marionebl/svg-term-cli)) |
| [terminalizer](https://github.com/faressoft/terminalizer) | GIF | Node.js; large output |
| [vhs](https://github.com/charmbracelet/vhs) | GIF / PNG / WebM | Driven by a scripted `.tape`, not a live capture |
| `script(1)` / `scriptreplay(1)` | Raw typescript | Replays in a terminal only; no image |

## Limitations

The terminal emulator covers common ANSI/CSI sequences, alternate screen mode, SGR colors, scrolling, erasing, and DEC line drawing. It does not yet implement every terminal feature or full Unicode width handling.
