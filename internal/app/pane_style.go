package app

import (
	"strings"

	"github.com/shellcell/ttysvg/internal/svg"
	termemu "github.com/shellcell/ttysvg/internal/terminal"
)

type livePalette struct {
	palette svg.Palette
}

func newLivePalette(cfg Config) livePalette {
	return livePalette{palette: svg.NewPalette(cfg.Theme, cfg.Colors)}
}

func (p livePalette) borderStyle() liveStyle {
	return liveStyle{fg: liveColorFromHex(p.palette.Indexed(8)), bg: liveColorFromHex(p.palette.Background())}
}

func (p livePalette) backgroundStyle() liveStyle {
	return liveStyle{fg: liveColorFromHex(p.palette.Foreground()), bg: liveColorFromHex(p.palette.Background())}
}

func (p livePalette) statusStyle() liveStyle {
	return liveStyle{fg: liveColorFromHex(p.palette.Background()), bg: liveColorFromHex(p.palette.Foreground()), bold: true}
}

func (p livePalette) dimStyle() liveStyle {
	return liveStyle{fg: liveColorFromHex(p.palette.Indexed(8)), bg: liveColorFromHex(p.palette.Background()), dim: true}
}

func (p livePalette) style(style termemu.Style) liveStyle {
	resolved := p.palette.ResolveStyle(style)
	return liveStyle{
		fg:            liveColorFromHex(resolved.Fg),
		bg:            liveColorFromHex(resolved.Bg),
		bold:          style.Has(termemu.AttrBold),
		dim:           style.Has(termemu.AttrDim),
		italic:        style.Has(termemu.AttrItalic),
		underline:     style.Has(termemu.AttrUnderline),
		blink:         style.Has(termemu.AttrBlink),
		strikethrough: style.Has(termemu.AttrStrikethrough),
		overline:      style.Has(termemu.AttrOverline),
	}
}

// liveColor is a pre-parsed terminal color. Resolving the palette hex string to
// RGB once (here) keeps the per-cell render loop free of repeated hex parsing.
type liveColor struct {
	r, g, b uint8
	set     bool
}

func liveColorFromHex(hex string) liveColor {
	// Fast path: palette colors are already canonical "#rrggbb", so parse them
	// directly and avoid the normalizing allocation parseRGB performs.
	if r, g, b, ok := svg.HexToRGB(hex); ok {
		return liveColor{r: r, g: g, b: b, set: true}
	}
	if r, g, b, ok := parseRGB(hex); ok {
		return liveColor{r: r, g: g, b: b, set: true}
	}
	return liveColor{}
}

type liveStyle struct {
	fg            liveColor
	bg            liveColor
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	blink         bool
	strikethrough bool
	overline      bool
}

// appendSGR writes the SGR escape for this style directly into b, avoiding the
// byteWriter is the subset of *strings.Builder / *bytes.Buffer used by the SGR
// helpers, letting the render path reuse a bytes.Buffer while control drawing
// keeps using a strings.Builder.
type byteWriter interface {
	WriteString(string) (int, error)
	WriteByte(byte) error
	Write([]byte) (int, error)
}

// per-style slice/Join/Sprintf allocations of the old sgr() string builder.
func (s liveStyle) appendSGR(b byteWriter) {
	b.WriteString("\x1b[0")
	if s.bold {
		b.WriteString(";1")
	}
	if s.dim {
		b.WriteString(";2")
	}
	if s.italic {
		b.WriteString(";3")
	}
	if s.underline {
		b.WriteString(";4")
	}
	if s.blink {
		b.WriteString(";5")
	}
	if s.strikethrough {
		b.WriteString(";9")
	}
	if s.overline {
		b.WriteString(";53")
	}
	if s.fg.set {
		b.WriteString(";38;2;")
		writeUint8(b, s.fg.r)
		b.WriteByte(';')
		writeUint8(b, s.fg.g)
		b.WriteByte(';')
		writeUint8(b, s.fg.b)
	}
	if s.bg.set {
		b.WriteString(";48;2;")
		writeUint8(b, s.bg.r)
		b.WriteByte(';')
		writeUint8(b, s.bg.g)
		b.WriteByte(';')
		writeUint8(b, s.bg.b)
	}
	b.WriteByte('m')
}

func (s liveStyle) sgr() string {
	var b strings.Builder
	s.appendSGR(&b)
	return b.String()
}

// writeUint8 writes v (0-255) in decimal using only WriteByte, so it needs no
// scratch slice; passing a stack array to b.Write would force it to the heap
// because b is an interface.
func writeUint8(b byteWriter, v uint8) {
	if v >= 100 {
		b.WriteByte('0' + v/100)
		b.WriteByte('0' + v/10%10)
		b.WriteByte('0' + v%10)
		return
	}
	if v >= 10 {
		b.WriteByte('0' + v/10)
		b.WriteByte('0' + v%10)
		return
	}
	b.WriteByte('0' + v)
}

func parseRGB(color string) (uint8, uint8, uint8, bool) {
	color = parseHexColor(color)
	if color == "" {
		return 0, 0, 0, false
	}
	return svg.HexToRGB(color)
}
