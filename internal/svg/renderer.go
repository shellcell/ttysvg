package svg

import (
	"fmt"
	"html"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shellcell/ttysvg/internal/terminal"
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
	// Loop makes the recording replay indefinitely. When false the animation
	// plays once and freezes on the final screen.
	Loop bool
	// TotalDuration is the recording length. When looping, the loop period is
	// TotalDuration plus a short hold, and it must be known up front so each
	// reveal can be timed as a fraction of the period.
	TotalDuration time.Duration
	// EndHold is how long the completed final screen is shown before the loop
	// repeats. Zero means the default (loopEndHold).
	EndHold time.Duration
	// Static renders a single still terminal snapshot instead of an animation.
	Static bool
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
	if style.Has(terminal.AttrBold) && fg.Mode == terminal.ColorIndexed && fg.Index < 8 {
		fg.Index += 8
	}
	fgHex := p.ResolveColor(fg, true)
	bgHex := p.ResolveColor(style.Bg, false)
	if style.Has(terminal.AttrInverse) {
		fgHex, bgHex = bgHex, fgHex
	}
	if style.Has(terminal.AttrHidden) {
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
	classOf    map[string]string
	styleBlock string
	dynToken   map[string]string // non-palette color -> ready-to-emit ` class="cN"` once promoted
	dynSeen    map[string]int    // emission count per non-palette color, until promoted
	dynStyle   strings.Builder   // accumulated `.cN{fill:#...}` defs, flushed in End
	dynCount   int               // number of promoted dynamic classes
	scratch    []byte            // reused for formatting numbers without per-value allocation
	runeBuf    [utf8.UTFMax]byte // reused for UTF-8 encoding so it does not escape to the heap
	frames     int
	period     time.Duration // total loop period: last frame time plus loopEndHold
	loop       bool
	closed     bool
}

// loopEndHold is the default hold on the completed final screen before the
// animation repeats, so viewers can read the end state before it loops.
// Overridable via Config.EndHold (the -hold flag).
const loopEndHold = 2 * time.Second

// endHold returns the configured final-screen hold, defaulting to loopEndHold.
func (r *Renderer) endHold() time.Duration {
	if r.cfg.EndHold > 0 {
		return r.cfg.EndHold
	}
	return loopEndHold
}

// writeInt formats an integer coordinate straight into the output using the
// reused scratch buffer, avoiding a string allocation per value.
func (r *Renderer) writeInt(n int) error {
	r.scratch = strconv.AppendInt(r.scratch[:0], int64(n), 10)
	_, err := r.w.Write(r.scratch)
	return err
}

// gridX, gridY, and baselineY return integer pixel positions for the grid.
// Coordinates are rounded at the absolute position (not by rounding the cell
// size and multiplying) so cumulative drift stays under half a pixel, and rect
// widths/heights are taken as the difference of rounded edges so adjacent
// backgrounds still tile seamlessly. Integer coordinates noticeably shrink the
// per-cell x lists that dominate text output.
func (r *Renderer) gridX(col int) int {
	return int(math.Round(r.cfg.Padding + float64(col)*r.cfg.CellWidth))
}

func (r *Renderer) gridY(row int) int {
	return int(math.Round(r.cfg.Padding + float64(row)*r.cfg.CellHeight))
}

func (r *Renderer) baselineY(row int) int {
	return int(math.Round(r.cfg.Padding + float64(row)*r.cfg.CellHeight + r.cfg.FontSize))
}

func NewRenderer(w io.Writer, cfg Config) *Renderer {
	if cfg.CellWidth == 0 {
		cfg.CellWidth = cfg.FontSize * 0.62
	}
	if cfg.CellHeight == 0 {
		cfg.CellHeight = cfg.FontSize * 1.25
	}
	cfg.FontFamily = fontFamilyWithFallback(cfg.FontFamily)
	return newRenderer(w, cfg, NewPalette(cfg.Theme, cfg.Colors))
}

// fontFamilyWithFallback guarantees the SVG font stack ends in the default
// monospace fallbacks. The SVG is viewed on machines that rarely have the
// recording terminal's font installed (GitHub READMEs, docs sites), and
// without fallbacks those viewers get a browser-default proportional font
// that breaks the grid alignment.
func fontFamilyWithFallback(family string) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return DefaultFontFamily
	}
	// A stack ending in a generic monospace family (ours or the user's own)
	// already has a safety net; don't append a duplicate tail.
	if strings.HasSuffix(family, "monospace") {
		return family
	}
	return family + ", " + DefaultFontFamily
}

func newRenderer(w io.Writer, cfg Config, pal Palette) *Renderer {
	active := make([]terminal.Cell, cfg.Cols*cfg.Rows)
	for i := range active {
		active[i] = terminal.BlankCell()
	}
	classOf, styleBlock := buildColorClasses(pal)
	r := &Renderer{
		w:          w,
		cfg:        cfg,
		palette:    pal,
		activeRows: active,
		rowStart:   make([]time.Duration, cfg.Rows),
		classOf:    classOf,
		styleBlock: styleBlock,
		dynToken:   make(map[string]string),
		dynSeen:    make(map[string]int),
		loop:       cfg.Loop,
	}
	if cfg.Loop && cfg.TotalDuration > 0 {
		r.period = cfg.TotalDuration + r.endHold()
	}
	return r
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

// dynPromoteAt is the emission count at which a non-palette color is promoted
// from an inline fill to a shared CSS class. A class costs a one-time
// definition plus a per-use reference that is shorter than the inline hex, so
// promotion only pays off for colors that repeat. The threshold keeps rarely
// used colors (e.g. a 256-color gradient where every cell differs) inline so
// they never regress, while frequently repeated theme/truecolor fills collapse
// to a class. dynClassCap bounds the worst-case style block.
const (
	dynPromoteAt = 8
	dynClassCap  = 256
)

// fillToken returns the fill attribute for a color: nothing when it matches the
// group's default (foreground), a class reference for palette colors, and for
// other colors (256-cube / truecolor) an inline fill until the color has been
// emitted dynPromoteAt times, after which it is promoted to a shared class.
func (r *Renderer) fillToken(color string) string {
	if color == r.palette.foreground {
		return ""
	}
	if token, ok := r.classOf[color]; ok {
		return token
	}
	if token, ok := r.dynToken[color]; ok {
		return token
	}
	r.dynSeen[color]++
	if r.dynSeen[color] >= dynPromoteAt && r.dynCount < dynClassCap {
		name := "c" + strconv.Itoa(r.dynCount)
		r.dynCount++
		delete(r.dynSeen, color)
		token := ` class="` + name + `"`
		r.dynToken[color] = token
		r.dynStyle.WriteString(".")
		r.dynStyle.WriteString(name)
		r.dynStyle.WriteString("{fill:")
		r.dynStyle.WriteString(color)
		r.dynStyle.WriteString("}")
		return token
	}
	return ` fill="` + color + `"`
}

func (r *Renderer) Begin() error {
	width := strconv.Itoa(int(math.Round(r.cfg.Padding*2 + float64(r.cfg.Cols)*r.cfg.CellWidth)))
	height := strconv.Itoa(int(math.Round(r.cfg.Padding*2 + float64(r.cfg.Rows)*r.cfg.CellHeight)))
	title := "Terminal recording"
	desc := "Animated terminal recording generated by ttysvg."
	if r.cfg.Static {
		title = "Terminal snapshot"
		desc = "Static terminal snapshot generated by ttysvg."
	}
	// The content group carries the default fill (foreground) so the most common
	// text and the cursor rect can omit a fill/class attribute entirely.
	const template = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s" role="img"><title>%s</title><desc>%s</desc><rect width="100%%" height="100%%" fill="%s"/><g font-family="%s" font-size="%s" fill="%s" text-rendering="geometricPrecision" shape-rendering="crispEdges" xml:space="preserve">`
	if _, err := fmt.Fprintf(r.w, template, width, height, width, height, title, desc, r.palette.background, html.EscapeString(r.cfg.FontFamily), formatFloat(r.cfg.FontSize), r.palette.foreground); err != nil {
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
	// Fallback when the period was not supplied up front (e.g. tests): derive it
	// from the final frame time. Set before updateRows so the non-final reveals it
	// emits use the same period as the final reveals below.
	if r.period <= 0 {
		r.period = begin + r.endHold()
	}
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

func (r *Renderer) WriteStaticFrame(frame terminal.Frame) error {
	r.frames++
	r.cursorX = frame.CursorX
	r.cursorY = frame.CursorY
	r.cursorVis = frame.CursorVisible
	for row := 0; row < r.cfg.Rows; row++ {
		if err := r.emitStaticRow(row, frame.Row(row)); err != nil {
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
	if _, err := io.WriteString(r.w, "</g>"); err != nil {
		return err
	}
	// Flush the classes promoted for frequently repeated non-palette colors. CSS
	// applies regardless of element order, so this trailing <style> styles the
	// class references already emitted above.
	if r.dynStyle.Len() > 0 {
		if _, err := io.WriteString(r.w, "<style>"+r.dynStyle.String()+"</style>"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(r.w, "</svg>")
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
	// A row whose cells all show the default background with no glyphs (and no
	// cursor) renders as nothing: the document background rect already paints
	// it, and reveal intervals never overlap, so the previous content of this
	// row is hidden by its own animation ending rather than by a cover rect.
	if !r.rowHasContent(row, cells) {
		return nil
	}

	// Rows start hidden (opacity 0); an animation reveals them for their interval.
	// opacity 0/1 renders identically to visibility hidden/visible here but the
	// attribute name and values are shorter.
	if _, err := io.WriteString(r.w, `<g opacity="0">`); err != nil {
		return err
	}
	if !r.loop {
		// Play once: reveal at the absolute time and freeze the final state.
		if final {
			if _, err := fmt.Fprintf(r.w, `<set attributeName="opacity" to="1" begin="%s" fill="freeze"/>`, formatTime(begin)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(r.w, `<set attributeName="opacity" to="1" begin="%s" dur="%s"/>`, formatTime(begin), formatTime(end-begin)); err != nil {
				return err
			}
		}
	} else if err := r.emitLoopReveal(begin, end, final); err != nil {
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
	_, err := io.WriteString(r.w, "</g>")
	return err
}

func (r *Renderer) emitStaticRow(row int, cells []terminal.Cell) error {
	if !r.rowHasContent(row, cells) {
		return nil
	}
	if err := r.emitBackgrounds(row, cells); err != nil {
		return err
	}
	if err := r.emitText(row, cells); err != nil {
		return err
	}
	return r.emitCursor(row, cells)
}

// emitLoopReveal emits an independent discrete opacity animation that makes the
// row's content visible only during [begin, end) of every loop iteration, where
// the loop period is r.period. Using one self-contained
// repeatCount="indefinite" animation per reveal — rather than a shared timebase
// referenced by syncbase offsets — keeps the loop correct across SMIL engines.
// Chrome, Firefox, and WebKit all repeat an independent animation reliably,
// whereas a self-referencing clock (begin="0;tb.end") with syncbase offsets
// desynchronizes or drops frames on later iterations in some engines.
func (r *Renderer) emitLoopReveal(begin, end time.Duration, final bool) error {
	period := r.period
	if period <= 0 {
		period = r.endHold()
	}
	if final {
		end = period
	}
	if begin < 0 {
		begin = 0
	}
	if end > period {
		end = period
	}
	startsAtZero := begin <= 0
	endsAtPeriod := end >= period
	var values, keyTimes string
	switch {
	case startsAtZero && endsAtPeriod:
		// Visible for the whole period. Two keyframes (rather than a lone value)
		// so every engine treats it as a valid constant animation.
		values, keyTimes = "1;1", "0;1"
	case startsAtZero:
		values, keyTimes = "1;0", "0;"+formatFraction(end, period)
	case endsAtPeriod:
		values, keyTimes = "0;1", "0;"+formatFraction(begin, period)
	default:
		values = "0;1;0"
		keyTimes = "0;" + formatFraction(begin, period) + ";" + formatFraction(end, period)
	}
	_, err := fmt.Fprintf(r.w,
		`<animate attributeName="opacity" calcMode="discrete" dur="%s" repeatCount="indefinite" values="%s" keyTimes="%s"/>`,
		formatTime(period), values, keyTimes)
	return err
}

// formatFraction renders t/period as a fraction in [0,1] with enough precision
// to preserve millisecond boundaries, trimming trailing zeros.
func formatFraction(t, period time.Duration) string {
	if period <= 0 {
		return "0"
	}
	f := float64(t) / float64(period)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return trimFloat(strconv.FormatFloat(f, 'f', 6, 64))
}

// writeRect emits a <rect> using writeInt for the geometry and a cached fill
// token, so a full frame's rects allocate nothing for their coordinates.
func (r *Renderer) writeRect(x, y, w, h int, fill string) error {
	if _, err := io.WriteString(r.w, `<rect x="`); err != nil {
		return err
	}
	if err := r.writeInt(x); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" y="`); err != nil {
		return err
	}
	if err := r.writeInt(y); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" width="`); err != nil {
		return err
	}
	if err := r.writeInt(w); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" height="`); err != nil {
		return err
	}
	if err := r.writeInt(h); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `"`); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, fill); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, `/>`)
	return err
}

// rowHasContent reports whether emitting this row would paint anything: a
// visible glyph, a non-default cell background, or the cursor.
func (r *Renderer) rowHasContent(row int, cells []terminal.Cell) bool {
	if r.cursorVis && r.cursorY == row && r.cursorX >= 0 && r.cursorX < len(cells) {
		return true
	}
	for i := range cells {
		if textCellVisible(cells[i]) {
			return true
		}
		if _, bg := r.colors(cells[i].Style); bg != r.palette.background {
			return true
		}
	}
	return false
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
		x := r.gridX(start)
		y := r.gridY(row)
		if err := r.writeRect(x, y, r.gridX(col)-x, r.gridY(row+1)-y, r.fillToken(bg)); err != nil {
			return err
		}
	}
	return nil
}

// textGapMax is the longest run of invisible cells (spaces, hidden cells, wide
// continuations) bridged inside one text run. Gap cells contribute nothing to
// the output — every glyph carries its own x position, so the run simply
// continues at the next visible cell's x. (Emitting space glyphs instead would
// break in WebKit, which collapses them despite xml:space="preserve" and then
// mis-assigns the x list.) Splitting costs a new <text> element (~35 bytes),
// so bridging is always cheaper; the cap just keeps runs local.
const textGapMax = 6

func (r *Renderer) emitText(row int, cells []terminal.Cell) error {
	for col := 0; col < len(cells); {
		for col < len(cells) && !textCellVisible(cells[col]) {
			col++
		}
		if col >= len(cells) {
			break
		}
		if shape, ok := blockCellShape(cells[col]); ok {
			next, err := r.emitBlockRun(row, col, cells, shape)
			if err != nil {
				return err
			}
			col = next
			continue
		}
		start := col
		key := r.textKey(cells[col].Style)
		end := col + 1
		col++
		// A cell longer than one UTF-16 code unit must be the last glyph of
		// its run: engines disagree on whether x-list entries advance per code
		// point (Chrome, WebKit) or per code unit (Gecko), so any glyph after
		// a supplementary-plane rune or combining sequence would shift in one
		// of them.
		for singleUnit(cells[end-1]) && col < len(cells) {
			if textCellVisible(cells[col]) {
				if _, ok := blockCellShape(cells[col]); ok {
					break
				}
				if r.textKey(cells[col].Style) != key {
					break
				}
				col++
				end = col
				continue
			}
			// Bridge a short invisible gap when the same style resumes after
			// it. Decorated styles cannot bridge: the injected spaces would
			// paint the underline/strikethrough across cells that had none.
			if key.decorated() {
				break
			}
			gap := col + 1
			for gap < len(cells) && !textCellVisible(cells[gap]) {
				gap++
			}
			if gap-col > textGapMax || gap >= len(cells) || r.textKey(cells[gap].Style) != key {
				break
			}
			if _, ok := blockCellShape(cells[gap]); ok {
				break
			}
			col = gap
		}
		y := r.baselineY(row)
		if _, err := io.WriteString(r.w, `<text x="`); err != nil {
			return err
		}
		// One x entry per emitted glyph: gap cells are skipped in both the x
		// list and the glyph string, keeping them aligned one-to-one.
		for cell := start; cell < end; cell++ {
			if !textCellVisible(cells[cell]) {
				continue
			}
			if cell > start {
				if _, err := io.WriteString(r.w, ` `); err != nil {
					return err
				}
			}
			if err := r.writeInt(r.gridX(cell)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(r.w, `" y="`); err != nil {
			return err
		}
		if err := r.writeInt(y); err != nil {
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
		if key.blink && !r.cfg.Static {
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
		if _, err := io.WriteString(r.w, "</text>"); err != nil {
			return err
		}
	}
	return nil
}

// singleUnit reports whether the cell's emitted text is exactly one UTF-16
// code unit, the only content whose x-list consumption every SVG engine
// agrees on.
func singleUnit(cell terminal.Cell) bool {
	return cell.Rune() <= 0xFFFF && cell.Combining == ""
}

// blockShape describes a Block Elements glyph as a rectangle in cell-fraction
// coordinates plus the glyph's ink coverage (1 for solid blocks, partial for
// the ░▒▓ shades).
type blockShape struct {
	x0, y0, x1, y1 float64
	alpha          float64
}

// blockShapeFor maps a Block Elements rune (U+2580–U+2595) to its rectangle.
// These cells are drawn as exact rects instead of font glyphs: viewers rarely
// have the recording font, and fallback fonts draw bars and shades with the
// wrong ink coverage, visibly changing their color.
func blockShapeFor(ch rune) (blockShape, bool) {
	switch {
	case ch == 0x2580: // ▀ upper half
		return blockShape{0, 0, 1, 0.5, 1}, true
	case ch >= 0x2581 && ch <= 0x2588: // ▁▂▃▄▅▆▇█ lower eighths, full block
		return blockShape{0, 1 - float64(ch-0x2580)/8, 1, 1, 1}, true
	case ch >= 0x2589 && ch <= 0x258F: // ▉▊▋▌▍▎▏ left eighths
		return blockShape{0, 0, float64(0x2590-ch) / 8, 1, 1}, true
	case ch == 0x2590: // ▐ right half
		return blockShape{0.5, 0, 1, 1, 1}, true
	case ch == 0x2591: // ░ light shade
		return blockShape{0, 0, 1, 1, 0.25}, true
	case ch == 0x2592: // ▒ medium shade
		return blockShape{0, 0, 1, 1, 0.5}, true
	case ch == 0x2593: // ▓ dark shade
		return blockShape{0, 0, 1, 1, 0.75}, true
	case ch == 0x2594: // ▔ upper eighth
		return blockShape{0, 0, 1, 0.125, 1}, true
	case ch == 0x2595: // ▕ right eighth
		return blockShape{0.875, 0, 1, 1, 1}, true
	}
	return blockShape{}, false
}

// blockCellShape reports the rect shape for a cell drawn as a Block Elements
// rune. Cells with combining marks, blink, or line decorations stay on the
// text path so those effects keep rendering.
func blockCellShape(cell terminal.Cell) (blockShape, bool) {
	if cell.Combining != "" || cell.Style.Has(terminal.AttrBlink|terminal.AttrUnderline|terminal.AttrStrikethrough|terminal.AttrOverline) {
		return blockShape{}, false
	}
	return blockShapeFor(cell.Rune())
}

// emitBlockRun draws adjacent identical block cells as a single rect and
// returns the index after the run. Only full-width shapes merge; partial-width
// shapes leave uncovered cell area, so each emits its own rect.
func (r *Renderer) emitBlockRun(row int, start int, cells []terminal.Cell, shape blockShape) (int, error) {
	key := r.textKey(cells[start].Style)
	end := start + 1
	fullWidth := shape.x0 == 0 && shape.x1 == 1
	for fullWidth && end < len(cells) && textCellVisible(cells[end]) {
		next, ok := blockCellShape(cells[end])
		if !ok || next != shape || r.textKey(cells[end].Style) != key {
			break
		}
		end++
	}
	x := r.cellPxX(float64(start) + shape.x0)
	y := r.cellPxY(float64(row) + shape.y0)
	w := r.cellPxX(float64(end-1)+shape.x1) - x
	h := r.cellPxY(float64(row)+shape.y1) - y
	// Sub-cell shapes can round to nothing at small cell sizes; the terminal
	// still shows a hairline, so keep at least one pixel.
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	fill := r.fillToken(key.fg)
	alpha := shape.alpha
	if key.dim {
		alpha *= 0.72 // matches the text path's dim opacity
	}
	if alpha < 1 {
		fill += ` fill-opacity="` + trimFloat(strconv.FormatFloat(alpha, 'f', 4, 64)) + `"`
	}
	return end, r.writeRect(x, y, w, h, fill)
}

// cellPxX and cellPxY convert fractional grid coordinates to pixels with the
// same absolute-position rounding as gridX/gridY.
func (r *Renderer) cellPxX(col float64) int {
	return int(math.Round(r.cfg.Padding + col*r.cfg.CellWidth))
}

func (r *Renderer) cellPxY(row float64) int {
	return int(math.Round(r.cfg.Padding + row*r.cfg.CellHeight))
}

func (r *Renderer) emitCursor(row int, cells []terminal.Cell) error {
	if !r.cursorVis || r.cursorY != row || r.cursorX < 0 || r.cursorX >= len(cells) {
		return nil
	}
	x := r.gridX(r.cursorX)
	y := r.gridY(row)
	if err := r.writeRect(x, y, r.gridX(r.cursorX+1)-x, r.gridY(row+1)-y, r.fillToken(r.palette.foreground)); err != nil {
		return err
	}
	if !textCellVisible(cells[r.cursorX]) {
		return nil
	}
	textY := r.baselineY(row)
	if _, err := io.WriteString(r.w, `<text x="`); err != nil {
		return err
	}
	if err := r.writeInt(x); err != nil {
		return err
	}
	if _, err := io.WriteString(r.w, `" y="`); err != nil {
		return err
	}
	if err := r.writeInt(textY); err != nil {
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
	_, err := io.WriteString(r.w, "</text>")
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
	return textKey{
		fg:            fg,
		bold:          style.Has(terminal.AttrBold),
		dim:           style.Has(terminal.AttrDim),
		italic:        style.Has(terminal.AttrItalic),
		underline:     style.Has(terminal.AttrUnderline),
		blink:         style.Has(terminal.AttrBlink),
		strikethrough: style.Has(terminal.AttrStrikethrough),
		overline:      style.Has(terminal.AttrOverline),
	}
}

// decorated reports whether the style paints marks across the full cell width
// (so gap cells bridged into a run would visibly change).
func (k textKey) decorated() bool {
	return k.underline || k.strikethrough || k.overline
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
	return !cell.WideContinuation() && !cell.Style.Has(terminal.AttrHidden) && cell.Rune() != ' '
}

func (r *Renderer) writeEscapedCells(cells []terminal.Cell) error {
	for i := range cells {
		cell := cells[i]
		// Gap cells bridged into a merged run emit nothing; their x-list
		// entries are skipped too, so glyphs and positions stay one-to-one.
		if !textCellVisible(cell) {
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
