# ttysvg

`ttysvg` records an interactive terminal session and converts it to an animated SVG after the session exits.

The live recording path is intentionally small: bytes read from the child PTY are written to your real terminal and to a compact timestamped event log. ANSI parsing, snapshot sampling, diffing, and SVG generation run only after you type `exit` or the recorded command exits.

![ttysvg usage animation](/resources/ttysvg.svg)

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
-size COLSxROWS     recording size; omit either side to auto-fit the terminal (100x, x30, 100x30)
-frame dur          minimum time between SVG snapshots; default 80ms
-idle dur           capture after output silence; default 60ms; 0 disables
-font-family s      SVG CSS font-family; defaults to detected terminal font plus fallbacks
-font-size px       SVG font size; defaults to terminal font size or 14
-cell-width px      SVG cell width; defaults to font-size*0.62
-cell-height px     SVG cell height; defaults to font-size*1.25
-theme name         auto, dark, or light; default auto
-bg color           terminal background color during recording, e.g. #0d1117; also used as SVG background
-padding px         SVG background frame around the terminal grid; default 0
-minify             write SVG without optional whitespace
-no-query-terminal  do not query current terminal colors before recording
-no-clear           do not clear the terminal before recording starts
-version            print version and exit
-q                  suppress progress and summary
```

`-frame` and `-idle` take Go durations such as `80ms`, `1s`, or `1500us`.

When `-size` is set (including a width- or height-only form like `100x` or `x30`), `ttysvg` compares the requested recording size with the current terminal. If it is the same size, recording runs directly in the terminal as usual. If it is larger, recording does not start and `ttysvg` asks you to resize the terminal first. If it is smaller, `ttysvg` starts the child session in a visible pane so you can prepare before recording. Use the pane buttons or keyboard shortcuts: `Ctrl-R` starts/resumes, `Ctrl-P` pauses/resumes, and `Ctrl-Q` stops. The prepared screen and each resume screen are captured as static SVG frames, then later output animates from those states. Paused output is live and interactive but is not recorded.

When pane mode is active, `-padding` is also previewed inside the pane border using whole terminal cells, approximated from the configured SVG cell size.

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
