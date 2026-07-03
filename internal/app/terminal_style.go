package app

import (
	"strconv"
	"strings"

	"github.com/rabarbra/ttysvg/internal/svg"
)

// terminalStyle holds the colors, theme, and font reported by the live
// terminal via OSC queries (see terminal_query.go).
type terminalStyle struct {
	FontFamily string
	FontSize   float64
	Theme      string
	Colors     svg.Colors
}

func (s terminalStyle) empty() bool {
	return s.FontFamily == "" && s.FontSize == 0 && s.Theme == "" && colorsEmpty(s.Colors)
}

func (s *terminalStyle) merge(next terminalStyle) {
	if next.FontFamily != "" {
		s.FontFamily = next.FontFamily
	}
	if next.FontSize > 0 {
		s.FontSize = next.FontSize
	}
	if next.Theme != "" {
		s.Theme = next.Theme
	}
	if next.Colors.Background != "" {
		s.Colors.Background = next.Colors.Background
	}
	if next.Colors.Foreground != "" {
		s.Colors.Foreground = next.Colors.Foreground
	}
	for i, color := range next.Colors.ANSI {
		if color != "" {
			s.Colors.ANSI[i] = color
		}
	}
}

func colorsEmpty(colors svg.Colors) bool {
	if colors.Background != "" || colors.Foreground != "" {
		return false
	}
	for _, color := range colors.ANSI {
		if color != "" {
			return false
		}
	}
	return true
}

// cssFontFamilyWithFallback quotes a detected terminal font family for CSS
// and appends the default monospace fallback stack.
func cssFontFamilyWithFallback(family string) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return ""
	}
	if !strings.HasPrefix(family, "'") && !strings.HasPrefix(family, "\"") {
		family = "'" + strings.ReplaceAll(family, "'", "\\'") + "'"
	}
	return family + ", " + svg.DefaultFontFamily
}

// parseHexColor normalizes a hex color ("#RRGGBB", "RRGGBB", "0xRRGGBB",
// optionally quoted) to lowercase "#rrggbb", or "" if the value is not one.
func parseHexColor(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
			value = value[1 : len(value)-1]
		}
	}
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return ""
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return ""
		}
	}
	return "#" + strings.ToLower(value)
}

func themeFromBackground(color string) string {
	color = parseHexColor(color)
	if color == "" {
		return ""
	}
	r, _ := strconv.ParseInt(color[1:3], 16, 64)
	g, _ := strconv.ParseInt(color[3:5], 16, 64)
	b, _ := strconv.ParseInt(color[5:7], 16, 64)
	luminance := 0.2126*float64(r)/255 + 0.7152*float64(g)/255 + 0.0722*float64(b)/255
	if luminance > 0.55 {
		return "light"
	}
	return "dark"
}
