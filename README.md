# ttysvg

`ttysvg` records an interactive terminal session and converts it to an animated SVG after the session exits.

The live recording path is intentionally small: bytes read from the child PTY are written to your real terminal and to a compact timestamped event log. ANSI parsing, snapshot sampling, diffing, and SVG generation run only after you type `exit` or the recorded command exits.

![ttysvg usage animation](/resources/ttysvg.svg)

## Installation

```sh
# From source (Go 1.26+)
go install github.com/rabarbra/ttysvg/cmd/ttysvg@latest

# Homebrew
brew install exex-org/tap/ttysvg

# Nix flake
nix run github:rabarbra/ttysvg
```

Prebuilt binaries for Linux and macOS, plus `.deb`/`.rpm` packages, are attached to each [GitHub release](https://github.com/rabarbra/ttysvg/releases).

## Usage

```sh
go install ./cmd/ttysvg
ttysvg -o out.svg
```

With no command, `ttysvg` starts your shell inside a recorder PTY. Use it normally, then type `exit` to stop recording. After that, `ttysvg` converts the captured PTY stream and prints the absolute SVG path, file size, frame count, and duration.

`-o` accepts a file or directory:

```sh
ttysvg -o out.svg
ttysvg -o ./recordings/
ttysvg -o ./recordings
```

If `-o` is a directory, the file is named `ttysvg.svg`. By default, output is written to `./ttysvg.svg` in the directory where `ttysvg` was started.

Existing directories and non-existing paths without a file extension are treated as directories. Paths with a file extension, such as `out.svg`, are treated as files.

Before recording starts, `ttysvg` verifies that the output location is writable. If it is not writable and stdin is interactive, it asks for another path and offers locations under `/tmp`. Recording does not begin until this succeeds.

You can also record a specific command:

```sh
ttysvg -o demo.svg -- vim
ttysvg -o demo.svg -- go test ./...
```

## Options

```text
-o path             output SVG file or directory
-gz                 write a gzip-compressed .svgz file (also enabled by a .svgz output path)
-size COLSxROWS     recording size; omit either side to auto-fit the terminal (100x, x30, 100x30)
-fps n              target frames per second; sets the capture rate (overrides -frame/-idle); e.g. 30
-frame dur          minimum time between SVG snapshots; default 25ms (40fps)
-idle dur           capture after output silence; default 25ms; 0 disables
-font-family s      SVG CSS font-family; defaults to monospace fallbacks, or the font reported by -query-terminal
-font-size px       SVG output font size; does not change the live terminal font; default 14, or the size reported by -query-terminal
-cell-width px      SVG cell width; defaults to font-size*0.62
-cell-height px     SVG cell height; defaults to font-size*1.25
-theme name         auto, dark, or light; default auto
-bg color           terminal background color during recording, e.g. #0d1117; also used as SVG background
-padding px         SVG background frame around the terminal grid; default 0
-no-loop            play once and freeze the final screen instead of looping
-hold dur           how long the final screen is held before the loop repeats; default 2s
-cast path          convert an asciinema .cast file (v2/v3) to SVG instead of recording
-query-terminal     query the terminal for colors, theme, and font before recording; off by default
-no-clear           do not clear the terminal before recording starts
-autostart          in pane mode, begin recording immediately instead of waiting for Ctrl-\

-headless           record the requested size directly with no interactive pane; for scripting and CI
-version            print version and exit
-q                  suppress progress and summary
```

`-o -` streams the SVG to stdout (stdout must be redirected; the live session moves to stderr):

```sh
ttysvg -o - -- make test > out.svg
```

### Frame rate

By default `ttysvg` captures at **40 fps**. Recording only emits a frame when the screen actually changes, so the rate is a ceiling during animation and adds nothing to idle stretches — higher rates cost size only while something is moving.

Use `-fps` to set the rate directly:

```sh
ttysvg -fps 60 -o smooth.svg     # 60 fps, smoother but larger
ttysvg -fps 15 -o small.svg      # 15 fps, choppier but smaller
```

`-fps` is a convenience that sets both the frame interval and the idle-capture interval to `1/fps`; it cannot be combined with `-frame`/`-idle`. For finer control, set those directly as Go durations such as `33ms`, `1s`, or `1500us`:

- `-frame` is the minimum time between captured frames (the rate ceiling during continuous output).
- `-idle` captures a settled frame after this much output silence; `-idle 0` disables it.

Roughly, raw SVG size scales with the frame rate, but the gzipped size grows far more slowly (the repeated markup compresses well), so serving the SVG gzip-encoded keeps even 60 fps recordings small.

Terminal identification is off by default to minimize startup latency. Pass `-query-terminal` to detect the terminal's colors, theme, and font with live OSC terminal queries (OSC 10/11 for foreground/background, OSC 4 for the ANSI palette, OSC 50 for the font where supported). The query ends as soon as the terminal answers, so it usually adds only a few milliseconds.

When `-size` is set (including a width- or height-only form like `100x` or `x30`), `ttysvg` compares the requested recording size with the current terminal. If it is the same size, recording runs directly in the terminal as usual. If it is larger, recording does not start and `ttysvg` asks you to resize the terminal first. If it is smaller, `ttysvg` starts the child session in a visible pane so you can prepare before recording. Use the pane buttons or keyboard shortcuts: `Ctrl-\` starts, pauses, and resumes (one toggle), and `Ctrl-]` stops — the keys are always shown in the pane status bar. These two are the least-contended control keys: `Ctrl-\` otherwise only means SIGQUIT (and is also asciinema's pause key), and `Ctrl-]` is telnet's own escape key; neither is used by shells, readline, tmux, or fzf. They are intercepted only while the pane is active — direct-mode recording filters nothing, so recording telnet itself still works there, and the mouse buttons always remain available. The prepared screen and each resume screen are captured as static SVG frames, then later output animates from those states. Paused output is live and interactive but is not recorded.

When pane mode is active, `-padding` is also previewed inside the pane border using whole terminal cells, approximated from the configured SVG cell size. Pass `-autostart` to skip the `Ctrl-\` wait and begin recording as soon as the pane opens; `Ctrl-\` and `Ctrl-]` still pause and stop. To skip the pane entirely and record the requested size straight away, even on an interactive terminal, use `-headless`.

The recorded SVG plays in an infinite loop by default: after the final screen is held briefly (2s, tune with `-hold`), the animation restarts from the beginning. Pass `-no-loop` to play once and freeze on the final screen instead.

## Converting asciinema recordings

`-cast` renders an existing [asciinema](https://asciinema.org/) recording (v2 or v3 `.cast`) through the same SVG pipeline, without recording anything:

```sh
ttysvg -cast demo.cast -o demo.svg
ttysvg -cast demo.cast -size 100x -o demo.svg   # override the header size
```

The terminal size comes from the cast header unless `-size` is given, and the header's `idle_time_limit` is respected.

## Continuous Integration

`ttysvg` runs without an interactive terminal, so it works in GitLab, GitHub Actions, and other CI runners. When stdout is not a TTY it records directly (no pane, no `Ctrl-\`) and streams the child output to the job log as usual. Two rules apply in CI:

- You must pass a command after `--`; with no command and a non-interactive stdin, `ttysvg` exits with an error instead of launching a shell.
- Pass `-size` to pin the recording dimensions, since there is no terminal to measure (otherwise it falls back to 80x24).

If a runner does allocate a TTY but you still want a fixed-size, non-interactive recording, add `-headless`.

GitHub Actions:

```yaml
- uses: actions/setup-go@v5
  with: { go-version-file: go.mod }
- run: go install github.com/rabarbra/ttysvg/cmd/ttysvg@latest
- run: ttysvg -q -size 100x30 -o out.svg -- make test
- uses: actions/upload-artifact@v4
  with:
    name: terminal-recording
    path: out.svg
```

GitLab CI (using the published container image):

```yaml
record:
  image: ghcr.io/rabarbra/ttysvg:latest
  script:
    - ttysvg -q -size 100x30 -o out.svg -- make test
  artifacts:
    paths:
      - out.svg
```

## Comparison

`ttysvg` records a **live, interactive** session and writes a **self-contained, script-free animated SVG** as a single static binary. That combination is its niche; related tools trade off differently:

| Tool | Output | How you drive it | Notes |
| --- | --- | --- | --- |
| **ttysvg** | Animated SVG (no JS) | Record a live session | Single static Go binary; small record-time hot path |
| [termtosvg](https://github.com/nbedos/termtosvg) | Animated SVG | Live session | Python; the closest analog |
| [asciinema](https://asciinema.org/) (+ [agg](https://github.com/asciinema/agg) / [svg-term](https://github.com/marionebl/svg-term-cli)) | `.cast` JSON, needs a player or converter | Live session | De-facto standard; web player |
| [svg-term-cli](https://github.com/marionebl/svg-term-cli) | SVG from a `.cast` | Converts asciinema casts | Node.js; a converter, not a recorder |
| [terminalizer](https://github.com/faressoft/terminalizer) | GIF | Live session / config | Node.js; large output |
| [vhs](https://github.com/charmbracelet/vhs) | GIF / PNG / WebM | Scripted `.tape` file | Great for repeatable demos, not a live capture |
| `script(1)` / `scriptreplay(1)` | Raw typescript | Live session | Replay in a terminal only; no image |

Because the SVG is a normal image with declarative animation and no scripts, it embeds in places that reject interactive SVG (such as GitHub READMEs). The markup is deliberately compact — per-color CSS classes, row diffing, and trimmed numbers — and because it is highly repetitive it compresses extremely well, typically to well under 10% of its raw size.

The `-gz` flag (or a `.svgz` output path) writes that gzip-compressed form directly:

```sh
ttysvg -gz -o demo.svg -- make test   # writes demo.svgz
ttysvg -o demo.svgz -- make test      # same thing
```

`.svgz` is best for **self-hosted pages and CI artifacts**, where you control the server or just want smaller files on disk. It is **not** suitable for GitHub READMEs: GitHub serves `.svgz` without the `Content-Encoding: gzip` header browsers need, so the image breaks. For GitHub, commit the plain `.svg` — GitHub's CDN already gzip-compresses it in transit, so you get the smaller transfer for free.

## Performance Model

In direct mode, recording avoids terminal emulation and SVG work while the TUI is running. The hot path does only this:

```text
PTY read -> stdout write -> buffered event-log write
```

The event log stores timestamp varints plus raw PTY byte chunks, so memory use during recording is bounded by fixed I/O buffers. The potentially expensive work happens after the process exits:

```text
event log -> ANSI terminal replay -> sampled frames -> streaming row-diff SVG
```

The SVG renderer does not keep all snapshots in RAM. It keeps the current terminal grid and active row states, then emits a row interval when that row changes. This keeps memory roughly proportional to terminal size rather than recording length.

Pane mode is heavier because it live-renders the child PTY through ttysvg's terminal emulator so the fixed-size pane, controls, pause/resume, and mouse translation remain visible. Memory is still bounded by terminal size, but CPU cost is higher than direct mode.

## SVG Compatibility

The output SVG uses declarative SVG animation and no scripts. This is intended to work as a normal image, including places that reject embedded interactive SVG content.

## Limitations

The terminal emulator covers common ANSI/CSI sequences, alternate screen mode, SGR colors, scrolling, erasing, and DEC line drawing. It does not yet implement every terminal feature or full Unicode width handling.
