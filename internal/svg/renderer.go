package svg

import (
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rabarbra/ttysvg/internal/terminal"
)

type Config struct {
	Cols       int
	Rows       int
	Theme      string
	FontSize   float64
	FontFamily string
	Colors     Colors
	CellWidth  float64
	CellHeight float64
	Padding    float64
}

type Colors struct {
	Background string
	Foreground string
	ANSI       [16]string
}

const DefaultFontFamily = "'JetBrainsMono Nerd Font Mono', 'JetBrainsMono Nerd Font', 'MesloLGS NF', 'Hack Nerd Font Mono', 'FiraCode Nerd Font Mono', 'CaskaydiaCove Nerd Font Mono', 'Symbols Nerd Font Mono', 'Symbols Nerd Font', ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace"

func DefaultColors(theme string) Colors {
	pal := NewPalette(theme, Colors{})
	return Colors{Background: pal.background, Foreground: pal.foreground, ANSI: pal.ansi}
}

type ResolvedStyle struct {
	Fg string
	Bg string
}

type Palette struct {
	background string
	foreground string
	ansi       [16]string
}

func NewPalette(theme string, colors Colors) Palette {
	pal := darkPalette()
	if theme == "light" {
		pal = lightPalette()
	}
	return pal.withColors(colors)
}

func (p Palette) Background() string {
	return p.background
}

func (p Palette) Foreground() string {
	return p.foreground
}

func (p Palette) Indexed(index uint8) string {
	if index < 16 {
		return p.ansi[index]
	}
	if index >= 232 {
		gray := uint8(8 + int(index-232)*10)
		return RGB(gray, gray, gray)
	}
	i := int(index - 16)
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	return RGB(levels[i/36], levels[(i/6)%6], levels[i%6])
}

func (p Palette) ResolveColor(color terminal.Color, foreground bool) string {
	switch color.Mode {
	case terminal.ColorIndexed:
		return p.Indexed(color.Index)
	case terminal.ColorRGB:
		return RGB(color.R, color.G, color.B)
	default:
		if foreground {
			return p.foreground
		}
		return p.background
	}
}

func (p Palette) ResolveStyle(style terminal.Style) ResolvedStyle {
	fg := style.Fg
	if style.Bold && fg.Mode == terminal.ColorIndexed && fg.Index < 8 {
		fg.Index += 8
	}
	fgHex := p.ResolveColor(fg, true)
	bgHex := p.ResolveColor(style.Bg, false)
	if style.Inverse {
		fgHex, bgHex = bgHex, fgHex
	}
	if style.Hidden {
		fgHex = bgHex
	}
	return ResolvedStyle{Fg: fgHex, Bg: bgHex}
}

func HexToRGB(color string) (uint8, uint8, uint8, bool) {
	if len(color) != 7 || color[0] != '#' {
		return 0, 0, 0, false
	}
	r, okR := hexByte(color[1:3])
	g, okG := hexByte(color[3:5])
	b, okB := hexByte(color[5:7])
	return r, g, b, okR && okG && okB
}

type Renderer struct {
	w          io.Writer
	cfg        Config
	palette    Palette
	activeRows []terminal.Cell
	rowStart   []time.Duration
	cursorX    int
	cursorY    int
	cursorVis  bool
	nl         string
	classOf    map[string]string
	styleBlock string
	scratch    []byte            // reused for formatting numbers without per-value allocation
	runeBuf    [utf8.UTFMax]byte // reused for UTF-8 encoding so it does not escape to the heap
	frames     int
	closed     bool
}

// writeFloat formats v (two decimals, trailing zeros trimmed) straight into the
// output using a reused scratch buffer, avoiding the string allocation that
// formatFloat incurs on every coordinate.
func (r *Renderer) writeFloat(v float64) error {
	r.scratch = appendTrimmedFloat(r.scratch[:0], v)
	_, err := r.w.Write(r.scratch)
	return err
}

func appendTrimmedFloat(dst []byte, v float64) []byte {
	dst = strconv.AppendFloat(dst, v, 'f', 2, 64)
	end := len(dst)
	for end > 0 && dst[end-1] == '0' {
		end--
	}
	if end > 0 && dst[end-1] == '.' {
		end--
	}
	return dst[:end]
}

func NewRenderer(w io.Writer, cfg Config) *Renderer {
	if cfg.CellWidth == 0 {
		cfg.CellWidth = cfg.FontSize * 0.62
	}
	if cfg.CellHeight == 0 {
		cfg.CellHeight = cfg.FontSize * 1.25
	}
	if cfg.FontFamily == "" {
		cfg.FontFamily = DefaultFontFamily
	}
	return newRenderer(w, cfg, NewPalette(cfg.Theme, cfg.Colors))
}

func newRenderer(w io.Writer, cfg Config, pal Palette) *Renderer {
	active := make([]terminal.Cell, cfg.Cols*cfg.Rows)
	for i := range active {
		active[i] = terminal.BlankCell()
	}
	classOf, styleBlock := buildColorClasses(pal)
	return &Renderer{
		w:          w,
		cfg:        cfg,
		palette:    pal,
		activeRows: active,
		rowStart:   make([]time.Duration, cfg.Rows),
		classOf:    classOf,
		styleBlock: styleBlock,
	}
}

// buildColorClasses assigns a short CSS class to each palette color so the most
// common fills are referenced as class="fg" instead of repeating the 7-byte hex
// on every element. Colors outside the palette (256-cube / truecolor) fall back
// to an inline fill.
func buildColorClasses(pal Palette) (map[string]string, string) {
	classOf := make(map[string]string)
	var style strings.Builder
	style.WriteString("<style>")
	add := func(name, color string) {
		if color == "" {
			return
		}
		if _, ok := classOf[color]; ok {
			return
		}
		// Store the ready-to-emit attribute so fillToken never concatenates.
		classOf[color] = ` class="` + name + `"`
		style.WriteString(".")
		style.WriteString(name)
		style.WriteString("{fill:")
		style.WriteString(color)
		style.WriteString("}")
	}
	// The foreground is the content group's default fill, so it needs no class.
	add("bg", pal.background)
	for i, color := range pal.ansi {
		add("a"+strconv.Itoa(i), color)
	}
	style.WriteString("</style>")
	return classOf, style.String()
}

// fillToken returns the fill attribute for a color: nothing when it matches the
// group's default (foreground), a class reference for other palette colors, and
// an inline fill for colors outside the palette (256-cube / truecolor).
func (r *Renderer) fillToken(color string) string {
	if color == r.palette.foreground {
		return ""
	}
	if token, ok := r.classOf[color]; ok {
		return token
	}
	return ` fill="` + color + `"`
}

func (r *Renderer) Begin() error {
	width := r.cfg.Padding*2 + float64(r.cfg.Cols)*r.cfg.CellWidth
	height := r.cfg.Padding*2 + float64(r.cfg.Rows)*r.cfg.CellHeight
	// The content group carries the default fill (foreground) so the most common
	// text and the cursor rect can omit a fill/class attribute entirely.
	const template = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s" role="img"><title>Terminal recording</title><desc>Animated terminal recording generated by ttysvg.</desc><rect width="100%%" height="100%%" fill="%s"/><g font-family="%s" font-size="%s" fill="%s" text-rendering="geometricPrecision" shape-rendering="crispEdges" xml:space="preserve">`
	if _, err := fmt.Fprintf(r.w, template, formatFloat(width), formatFloat(height), formatFloat(width), formatFloat(height), r.palette.background, html.EscapeString(r.cfg.FontFamily), formatFloat(r.cfg.FontSize), r.palette.foreground); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, r.styleBlock)
	return err
}

func (r *Renderer) WriteFrame(frame terminal.Frame, begin time.Duration, _ time.Duration) error {
	r.frames++
	return r.updateRows(frame, begin)
}

func (r *Renderer) WriteFinalFrame(frame terminal.Frame, begin time.Duration) error {
	r.frames++
	if err := r.updateRows(frame, begin); err != nil {
		return err
	}
	for row := 0; row < r.cfg.Rows; row++ {
		if err := r.emitRow(row, r.row(row), r.rowStart[row], 0, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) End() error {
	if r.closed {
		return nil
	}
	r.closed = true
	_, err := io.WriteString(r.w, "</g>"+r.nl+"</svg>"+r.nl)
	return err
}

func (r *Renderer) FrameCount() int {
	return r.frames
}

func (r *Renderer) updateRows(frame terminal.Frame, begin time.Duration) error {
	for row := 0; row < r.cfg.Rows; row++ {
		current := r.row(row)
		next := frame.Row(row)
		oldCursor := r.cursorVis && r.cursorY == row
		newCursor := frame.CursorVisible && frame.CursorY == row
		cursorEqual := oldCursor == newCursor && (!oldCursor || r.cursorX == frame.CursorX)
		if cellsEqual(current, next) && cursorEqual {
			continue
		}
		if err := r.emitRow(row, current, r.rowStart[row], begin, false); err != nil {
			return err
		}
		copy(current, next)
		r.rowStart[row] = begin
	}
	r.cursorX = frame.CursorX
	r.cursorY = frame.CursorY
	r.cursorVis = frame.CursorVisible
	return nil
}

func (r *Renderer) row(row int) []terminal.Cell {
	start := row * r.cfg.Cols
	return r.activeRows[start : start+r.cfg.Cols]
}

func (r *Renderer) emitRow(row int, cells []terminal.Cell, begin time.Duration, end time.Duration, final bool) error {
	if !final && end <= begin {
		return nil
	}

	// Rows start hidden (opacity 0) and a <set> reveals them for their interval.
	// opacity 0/1 renders identically to visibility hidden/visible here but the
	// attribute name and values are shorter, and it repeats on every frame row.
	if _, err := io.WriteString(r.w, `<g opacity="0">`+r.nl); err != nil {
		return err
	}
	if final {
		if _, err := fmt.Fprintf(r.w, `<set attributeName="opacity" to="1" begin="%s" fill="freeze"/>`+r.nl, formatTime(begin)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(r.w, `<set attributeName="opacity" to="1" begin="%s" dur="%s"/>`+r.nl, formatTime(begin), formatTime(end-begin)); err != nil {
			return err
		}
	}
	if err := r.emitRowBackground(row); err != nil {
		return err
	}
	if err := r.emitBackgrounds(row, cells); err != nil {
		return err
	}
	if err := r.emitText(row, cells); err != nil {
		return err
	}
	if err := r.emitCursor(row, cells); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, "</g>"+r.nl)
	return err
}

// writeRect emits a <rect> using writeFloat for the geometry and a cached fill
// token, so a full frame's rects allocate nothing for their coordinates.
func (r *Renderer) writeRect(x, y, w, h float64, fill string) error {
	if _, err := io.WriteString(r.w, `<rect x="`); err != nil {
		return err
	}
	if err := r.writeFloat(x); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" y="`); err != nil {
		return err
	}
	if err := r.writeFloat(y); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" width="`); err != nil {
		return err
	}
	if err := r.writeFloat(w); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" height="`); err != nil {
		return err
	}
	if err := r.writeFloat(h); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `"`); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, fill); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, `/>`+r.nl)
	return err
}

func (r *Renderer) emitRowBackground(row int) error {
	y := r.cfg.Padding + float64(row)*r.cfg.CellHeight
	width := float64(r.cfg.Cols) * r.cfg.CellWidth
	return r.writeRect(r.cfg.Padding, y, width, r.cfg.CellHeight, r.fillToken(r.palette.background))
}

func (r *Renderer) emitBackgrounds(row int, cells []terminal.Cell) error {
	for col := 0; col < len(cells); {
		_, bg := r.colors(cells[col].Style)
		if bg == r.palette.background {
			col++
			continue
		}
		start := col
		for col < len(cells) {
			_, nextBG := r.colors(cells[col].Style)
			if nextBG != bg {
				break
			}
			col++
		}
		x := r.cfg.Padding + float64(start)*r.cfg.CellWidth
		y := r.cfg.Padding + float64(row)*r.cfg.CellHeight
		width := float64(col-start) * r.cfg.CellWidth
		if err := r.writeRect(x, y, width, r.cfg.CellHeight, r.fillToken(bg)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) emitText(row int, cells []terminal.Cell) error {
	for col := 0; col < len(cells); {
		for col < len(cells) && !textCellVisible(cells[col]) {
			col++
		}
		if col >= len(cells) {
			break
		}
		start := col
		key := r.textKey(cells[col].Style)
		for col < len(cells) && textCellVisible(cells[col]) && r.textKey(cells[col].Style) == key {
			col++
		}
		end := col
		y := r.cfg.Padding + float64(row)*r.cfg.CellHeight + r.cfg.FontSize
		if _, err := io.WriteString(r.w, `<text x="`); err != nil {
			return err
		}
		for cell := start; cell < end; cell++ {
			if cell > start {
				if _, err := io.WriteString(r.w, ` `); err != nil {
					return err
				}
			}
			x := r.cfg.Padding + float64(cell)*r.cfg.CellWidth
			if err := r.writeFloat(x); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(r.w, `" y="`); err != nil {
			return err
		}
		if err := r.writeFloat(y); err != nil {
			return err
		}
		if _, err := io.WriteString(r.w, `"`); err != nil {
			return err
		}
		if _, err := io.WriteString(r.w, r.fillToken(key.fg)); err != nil {
			return err
		}
		if key.bold {
			if _, err := io.WriteString(r.w, ` font-weight="700"`); err != nil {
				return err
			}
		}
		if key.italic {
			if _, err := io.WriteString(r.w, ` font-style="italic"`); err != nil {
				return err
			}
		}
		if decoration := key.textDecoration(); decoration != "" {
			if _, err := fmt.Fprintf(r.w, ` text-decoration="%s"`, decoration); err != nil {
				return err
			}
		}
		if key.dim {
			if _, err := io.WriteString(r.w, ` opacity="0.72"`); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(r.w, `>`); err != nil {
			return err
		}
		if key.blink {
			values := "1;0;1"
			if key.dim {
				values = "0.72;0;0.72"
			}
			if _, err := fmt.Fprintf(r.w, `<animate attributeName="opacity" values="%s" dur="1s" repeatCount="indefinite"/>`, values); err != nil {
				return err
			}
		}
		if err := r.writeEscapedCells(cells[start:end]); err != nil {
			return err
		}
		if _, err := io.WriteString(r.w, "</text>"+r.nl); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) emitCursor(row int, cells []terminal.Cell) error {
	if !r.cursorVis || r.cursorY != row || r.cursorX < 0 || r.cursorX >= len(cells) {
		return nil
	}
	x := r.cfg.Padding + float64(r.cursorX)*r.cfg.CellWidth
	y := r.cfg.Padding + float64(row)*r.cfg.CellHeight
	if err := r.writeRect(x, y, r.cfg.CellWidth, r.cfg.CellHeight, r.fillToken(r.palette.foreground)); err != nil {
		return err
	}
	if !textCellVisible(cells[r.cursorX]) {
		return nil
	}
	textY := y + r.cfg.FontSize
	if _, err := io.WriteString(r.w, `<text x="`); err != nil {
		return err
	}
	if err := r.writeFloat(x); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" y="`); err != nil {
		return err
	}
	if err := r.writeFloat(textY); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `"`); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, r.fillToken(r.palette.background)); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `>`); err != nil {
		return err
	}
	if err := r.writeEscapedCells(cells[r.cursorX : r.cursorX+1]); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, "</text>"+r.nl)
	return err
}

type textKey struct {
	fg            string
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	blink         bool
	strikethrough bool
	overline      bool
}

func (r *Renderer) textKey(style terminal.Style) textKey {
	fg, _ := r.colors(style)
	return textKey{fg: fg, bold: style.Bold, dim: style.Dim, italic: style.Italic, underline: style.Underline, blink: style.Blink, strikethrough: style.Strikethrough, overline: style.Overline}
}

func (k textKey) textDecoration() string {
	decoration := ""
	if k.underline {
		decoration = appendDecoration(decoration, "underline")
	}
	if k.strikethrough {
		decoration = appendDecoration(decoration, "line-through")
	}
	if k.overline {
		decoration = appendDecoration(decoration, "overline")
	}
	return decoration
}

func appendDecoration(current, next string) string {
	if current == "" {
		return next
	}
	return current + " " + next
}

func (r *Renderer) colors(style terminal.Style) (string, string) {
	resolved := r.palette.ResolveStyle(style)
	return resolved.Fg, resolved.Bg
}

func cellsEqual(a, b []terminal.Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func textCellVisible(cell terminal.Cell) bool {
	return !cell.WideContinuation && !cell.Style.Hidden && cell.Rune() != ' '
}

func (r *Renderer) writeEscapedCells(cells []terminal.Cell) error {
	for i := range cells {
		cell := cells[i]
		if cell.WideContinuation {
			continue
		}
		// Escape the rune directly instead of cell.Text(), which would allocate
		// a string per cell across the whole recording.
		if err := r.writeEscapedRune(cell.Rune()); err != nil {
			return err
		}
		if cell.Combining != "" {
			if err := r.writeEscapedString(cell.Combining); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) writeEscapedString(text string) error {
	for _, ch := range text {
		if err := r.writeEscapedRune(ch); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) writeEscapedRune(ch rune) error {
	switch ch {
	case '&':
		_, err := io.WriteString(r.w, "&amp;")
		return err
	case '<':
		_, err := io.WriteString(r.w, "&lt;")
		return err
	case '>':
		_, err := io.WriteString(r.w, "&gt;")
		return err
	default:
		// r.runeBuf lives in the heap-allocated Renderer, so slicing it here does
		// not force a per-rune allocation the way a stack array would.
		n := utf8.EncodeRune(r.runeBuf[:], ch)
		_, err := r.w.Write(r.runeBuf[:n])
		return err
	}
}

func formatTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return trimFloat(strconv.FormatFloat(float64(d)/float64(time.Second), 'f', 3, 64)) + "s"
}

func formatFloat(v float64) string {
	return trimFloat(strconv.FormatFloat(v, 'f', 2, 64))
}

// trimFloat drops trailing fractional zeros (and a bare trailing dot) from a
// fixed-precision number so 868.00 becomes 868 and 17.50 becomes 17.5.
func trimFloat(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func (p Palette) withColors(colors Colors) Palette {
	if colors.Background != "" {
		p.background = colors.Background
	}
	if colors.Foreground != "" {
		p.foreground = colors.Foreground
	}
	for i, color := range colors.ANSI {
		if color != "" {
			p.ansi[i] = color
		}
	}
	return p
}

func darkPalette() Palette {
	return Palette{
		background: "#0d1117",
		foreground: "#c9d1d9",
		ansi: [16]string{
			"#484f58", "#ff7b72", "#3fb950", "#d29922", "#58a6ff", "#bc8cff", "#39c5cf", "#b1bac4",
			"#6e7681", "#ffa198", "#56d364", "#e3b341", "#79c0ff", "#d2a8ff", "#56d4dd", "#f0f6fc",
		},
	}
}

func lightPalette() Palette {
	return Palette{
		background: "#ffffff",
		foreground: "#24292f",
		ansi: [16]string{
			"#57606a", "#cf222e", "#1a7f37", "#9a6700", "#0969da", "#8250df", "#1b7c83", "#6e7781",
			"#8c959f", "#a40e26", "#116329", "#7d4e00", "#0550ae", "#6639ba", "#3192aa", "#24292f",
		},
	}
}

func RGB(r, g, b uint8) string {
	return rgb(r, g, b)
}

func rgb(r, g, b uint8) string {
	const hex = "0123456789abcdef"
	out := []byte{'#', 0, 0, 0, 0, 0, 0}
	out[1] = hex[r>>4]
	out[2] = hex[r&0x0f]
	out[3] = hex[g>>4]
	out[4] = hex[g&0x0f]
	out[5] = hex[b>>4]
	out[6] = hex[b&0x0f]
	return string(out)
}

func hexByte(value string) (uint8, bool) {
	if len(value) != 2 {
		return 0, false
	}
	var out uint8
	for _, r := range value {
		out <<= 4
		switch {
		case r >= '0' && r <= '9':
			out += uint8(r - '0')
		case r >= 'a' && r <= 'f':
			out += uint8(r-'a') + 10
		case r >= 'A' && r <= 'F':
			out += uint8(r-'A') + 10
		default:
			return 0, false
		}
	}
	return out, true
}
