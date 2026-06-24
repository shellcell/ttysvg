# ttysvg

`ttysvg` records an interactive terminal session and converts it to an animated SVG after the session exits.

The live recording path is intentionally small: bytes read from the child PTY are written to your real terminal and to a compact timestamped event log. ANSI parsing, snapshot sampling, diffing, and SVG generation run only after you type `exit` or the recorded command exits.

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
-o path          output SVG file or directory
-size COLSxROWS fixed PTY size; defaults to current terminal size or 80x24
-frame-ms n     minimum time between SVG snapshots; default 80
-idle-ms n      capture after output silence; default 60
-font-family s  SVG CSS font-family; defaults to detected terminal font plus fallbacks
-theme name     auto, dark, or light; default auto
-minify         write SVG without optional whitespace
-query-terminal query current terminal colors before recording; default true
-clear          clear the terminal before recording starts; default true
-q              suppress progress and summary
```

## Performance Model

Recording avoids terminal emulation and SVG work while the TUI is running. The hot path does only this:

```text
PTY read -> stdout write -> buffered event-log write
```

The event log stores timestamp varints plus raw PTY byte chunks, so memory use during recording is bounded by fixed I/O buffers. The potentially expensive work happens after the process exits:

```text
event log -> ANSI terminal replay -> sampled frames -> streaming row-diff SVG
```

The SVG renderer does not keep all snapshots in RAM. It keeps the current terminal grid and active row states, then emits a row interval when that row changes. This keeps memory roughly proportional to terminal size rather than recording length.

## SVG Compatibility

The output SVG uses declarative SVG animation and no scripts. This is intended to work as a normal image, including places that reject embedded interactive SVG content.

## Limitations

The terminal emulator covers common ANSI/CSI sequences, alternate screen mode, SGR colors, scrolling, erasing, and DEC line drawing. It does not yet implement every terminal feature or full Unicode width handling.
