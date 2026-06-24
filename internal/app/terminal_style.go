package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rabarbra/ttysvg/internal/svg"
)

type terminalStyle struct {
	FontFamily string
	FontSize   float64
	Theme      string
	Colors     svg.Colors
}

type terminalDetector struct {
	name  string
	paths func() []string
	parse func([]string) terminalStyle
}

func detectTerminalStyle() terminalStyle {
	detectors := []terminalDetector{
		{name: "ghostty", paths: ghosttyConfigPaths, parse: parseGhosttyStyle},
		{name: "kitty", paths: kittyConfigPaths, parse: parseKittyStyle},
		{name: "wezterm", paths: wezTermConfigPaths, parse: parseWezTermStyle},
		{name: "alacritty", paths: alacrittyConfigPaths, parse: parseAlacrittyStyle},
		{name: "foot", paths: footConfigPaths, parse: parseFootStyle},
		{name: "konsole", paths: konsoleConfigPaths, parse: parseKonsoleStyle},
		{name: "windows-terminal", paths: windowsTerminalConfigPaths, parse: parseWindowsTerminalStyle},
		{name: "vscode", paths: vscodeConfigPaths, parse: parseVSCodeStyle},
	}

	active := activeTerminalNames()
	if len(active) > 0 {
		for _, detector := range detectors {
			if !active[detector.name] {
				continue
			}
			if style := detector.parse(detector.paths()); !style.empty() {
				return style.finalize()
			}
		}
		return terminalStyle{}
	}

	for _, detector := range detectors {
		if style := detector.parse(detector.paths()); !style.empty() {
			return style.finalize()
		}
	}
	return terminalStyle{}
}

func activeTerminalNames() map[string]bool {
	active := map[string]bool{}
	termProgram := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))

	if strings.Contains(termProgram, "ghostty") || strings.Contains(term, "ghostty") {
		active["ghostty"] = true
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "kitty") {
		active["kitty"] = true
	}
	if strings.Contains(termProgram, "wezterm") || os.Getenv("WEZTERM_EXECUTABLE") != "" || os.Getenv("WEZTERM_CONFIG_FILE") != "" {
		active["wezterm"] = true
	}
	if strings.Contains(termProgram, "alacritty") || strings.Contains(term, "alacritty") || os.Getenv("ALACRITTY_LOG") != "" {
		active["alacritty"] = true
	}
	if strings.Contains(termProgram, "foot") || strings.Contains(term, "foot") {
		active["foot"] = true
	}
	if os.Getenv("KONSOLE_VERSION") != "" || strings.Contains(termProgram, "konsole") {
		active["konsole"] = true
	}
	if os.Getenv("WT_SESSION") != "" || strings.Contains(termProgram, "windows_terminal") {
		active["windows-terminal"] = true
	}
	if strings.Contains(termProgram, "vscode") || os.Getenv("TERM_PROGRAM") == "vscode" {
		active["vscode"] = true
	}
	return active
}

func (s terminalStyle) empty() bool {
	return s.FontFamily == "" && s.FontSize == 0 && s.Theme == "" && colorsEmpty(s.Colors)
}

func (s terminalStyle) finalize() terminalStyle {
	if s.Theme == "" && s.Colors.Background != "" {
		s.Theme = themeFromBackground(s.Colors.Background)
	}
	return s
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

func cssFontFamilyWithFallback(fontFamily string) string {
	families := splitFontFamilies(fontFamily)
	if len(families) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(families)+1)
	for _, family := range families {
		quoted = append(quoted, quoteCSSFontFamily(family))
	}
	quoted = append(quoted, svg.DefaultFontFamily)
	return strings.Join(quoted, ", ")
}

func splitFontFamilies(value string) []string {
	var families []string
	for _, part := range splitCSV(value) {
		part = strings.TrimSpace(unquoteConfigValue(part))
		if part != "" {
			families = append(families, part)
		}
	}
	return families
}

func quoteCSSFontFamily(family string) string {
	lower := strings.ToLower(family)
	if strings.HasPrefix(family, "'") || strings.HasPrefix(family, "\"") || lower == "serif" || lower == "sans-serif" || lower == "monospace" || lower == "ui-monospace" {
		return family
	}
	return "'" + strings.ReplaceAll(family, "'", "\\'") + "'"
}

func ghosttyConfigPaths() []string {
	return existingPaths([]string{
		os.Getenv("GHOSTTY_CONFIG_FILE"),
		joinEnv("XDG_CONFIG_HOME", "ghostty", "config"),
		joinHome(".config", "ghostty", "config"),
		joinHome("Library", "Application Support", "com.mitchellh.ghostty", "config"),
		joinEnv("APPDATA", "ghostty", "config"),
		joinEnv("LOCALAPPDATA", "ghostty", "config"),
	})
}

func parseGhosttyStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		assignments, ok := parseAssignmentFile(path)
		if !ok {
			continue
		}
		var next terminalStyle
		next.FontFamily = assignments["font-family"]
		next.FontSize = parsePositiveFloat(assignments["font-size"])
		next.Theme = themeFromName(assignments["theme"])
		next.Colors.Background = parseHexColor(assignments["background"])
		next.Colors.Foreground = parseHexColor(assignments["foreground"])
		for key, value := range assignments {
			if strings.HasPrefix(key, "palette") || strings.HasPrefix(key, "color") {
				if index, color, ok := parseIndexedColor(key, value); ok && index >= 0 && index < len(next.Colors.ANSI) {
					next.Colors.ANSI[index] = color
				}
			}
		}
		style.merge(next)
	}
	return style
}

func kittyConfigPaths() []string {
	var paths []string
	if dir := os.Getenv("KITTY_CONFIG_DIRECTORY"); dir != "" {
		paths = append(paths, filepath.Join(dir, "kitty.conf"))
	}
	paths = append(paths,
		joinEnv("XDG_CONFIG_HOME", "kitty", "kitty.conf"),
		joinHome(".config", "kitty", "kitty.conf"),
		joinEnv("APPDATA", "kitty", "kitty.conf"),
	)
	return existingPaths(paths)
}

func parseKittyStyle(paths []string) terminalStyle {
	var style terminalStyle
	seen := map[string]bool{}
	for _, path := range paths {
		style.merge(parseKittyFile(path, seen))
	}
	return style
}

func parseKittyFile(path string, seen map[string]bool) terminalStyle {
	if seen[path] {
		return terminalStyle{}
	}
	seen[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		return terminalStyle{}
	}
	var style terminalStyle
	base := filepath.Dir(path)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripConfigComment(rawLine))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if key == "include" && value != "" {
			for _, include := range expandConfigPathList(value, base) {
				style.merge(parseKittyFile(include, seen))
			}
			continue
		}
		switch key {
		case "font_family":
			style.FontFamily = unquoteConfigValue(value)
		case "font_size":
			style.FontSize = parsePositiveFloat(value)
		case "background":
			style.Colors.Background = parseHexColor(value)
		case "foreground":
			style.Colors.Foreground = parseHexColor(value)
		default:
			if strings.HasPrefix(key, "color") {
				if index, color, ok := parseIndexedColor(key, value); ok && index >= 0 && index < len(style.Colors.ANSI) {
					style.Colors.ANSI[index] = color
				}
			}
		}
	}
	return style
}

func wezTermConfigPaths() []string {
	return existingPaths([]string{
		os.Getenv("WEZTERM_CONFIG_FILE"),
		joinHome(".wezterm.lua"),
		joinEnv("XDG_CONFIG_HOME", "wezterm", "wezterm.lua"),
		joinHome(".config", "wezterm", "wezterm.lua"),
		joinEnv("APPDATA", "wezterm", "wezterm.lua"),
	})
}

func parseWezTermStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		style.merge(parseWezTermConfig(string(data)))
	}
	return style
}

func parseWezTermConfig(data string) terminalStyle {
	var style terminalStyle
	if m := regexp.MustCompile(`(?m)font_size\s*=\s*([0-9.]+)`).FindStringSubmatch(data); len(m) == 2 {
		style.FontSize = parsePositiveFloat(m[1])
	}
	if m := regexp.MustCompile(`(?s)font\s*=\s*wezterm\.font(?:_with_fallback)?\s*\((.*?)\)`).FindStringSubmatch(data); len(m) == 2 {
		style.FontFamily = strings.Join(quotedStrings(m[1]), ", ")
	}
	if m := regexp.MustCompile(`(?m)color_scheme\s*=\s*["']([^"']+)["']`).FindStringSubmatch(data); len(m) == 2 {
		style.Theme = themeFromName(m[1])
	}
	style.Colors.Background = firstHexAssignment(data, "background")
	style.Colors.Foreground = firstHexAssignment(data, "foreground")
	if m := regexp.MustCompile(`(?s)ansi\s*=\s*\{(.*?)\}`).FindStringSubmatch(data); len(m) == 2 {
		for i, color := range quotedHexColors(m[1]) {
			if i < 8 {
				style.Colors.ANSI[i] = color
			}
		}
	}
	if m := regexp.MustCompile(`(?s)brights\s*=\s*\{(.*?)\}`).FindStringSubmatch(data); len(m) == 2 {
		for i, color := range quotedHexColors(m[1]) {
			if i < 8 {
				style.Colors.ANSI[i+8] = color
			}
		}
	}
	return style
}

func alacrittyConfigPaths() []string {
	return existingPaths([]string{
		joinEnv("ALACRITTY_CONFIG_FILE"),
		joinEnv("XDG_CONFIG_HOME", "alacritty", "alacritty.toml"),
		joinEnv("XDG_CONFIG_HOME", "alacritty", "alacritty.yml"),
		joinEnv("XDG_CONFIG_HOME", "alacritty", "alacritty.yaml"),
		joinHome(".config", "alacritty", "alacritty.toml"),
		joinHome(".config", "alacritty", "alacritty.yml"),
		joinHome(".config", "alacritty", "alacritty.yaml"),
		joinHome(".alacritty.yml"),
		joinEnv("APPDATA", "alacritty", "alacritty.toml"),
		joinEnv("APPDATA", "alacritty", "alacritty.yml"),
	})
}

func parseAlacrittyStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		style.merge(parseAlacrittyConfig(string(data)))
	}
	return style
}

func parseAlacrittyConfig(data string) terminalStyle {
	var style terminalStyle
	section := ""
	var stack []yamlStackEntry
	for _, rawLine := range strings.Split(data, "\n") {
		lineWithoutComment := stripConfigComment(rawLine)
		line := strings.TrimSpace(lineWithoutComment)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.TrimSpace(line[1:strings.Index(line, "]")])
			stack = nil
			continue
		}
		indent := len(lineWithoutComment) - len(strings.TrimLeft(lineWithoutComment, " \t"))
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		if value == "" && !strings.Contains(line, "=") {
			stack = pushYamlStack(stack, indent, key)
			continue
		}
		path := strings.ToLower(key)
		if section != "" {
			path = strings.ToLower(section + "." + key)
		} else if len(stack) > 0 {
			stack = popYamlStack(stack, indent)
			path = strings.ToLower(strings.Join(append(yamlStackNames(stack), key), "."))
		}
		applyPathStyleValue(&style, path, value)
	}
	return style
}

func footConfigPaths() []string {
	return existingPaths([]string{
		joinEnv("XDG_CONFIG_HOME", "foot", "foot.ini"),
		joinHome(".config", "foot", "foot.ini"),
	})
}

func parseFootStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		style.merge(parseFootConfig(string(data)))
	}
	return style
}

func parseFootConfig(data string) terminalStyle {
	var style terminalStyle
	section := ""
	for _, rawLine := range strings.Split(data, "\n") {
		line := strings.TrimSpace(stripConfigComment(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1:strings.Index(line, "]")]))
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		key = strings.ToLower(key)
		if section == "main" || section == "" {
			if key == "font" {
				family, size := parseFootFont(value)
				style.FontFamily = family
				style.FontSize = size
			} else if key == "font-size" {
				style.FontSize = parsePositiveFloat(value)
			}
		}
		if section == "colors" {
			switch key {
			case "background":
				style.Colors.Background = parseHexColor(value)
			case "foreground":
				style.Colors.Foreground = parseHexColor(value)
			default:
				if strings.HasPrefix(key, "regular") || strings.HasPrefix(key, "bright") {
					if index, color, ok := parseIndexedColor(key, value); ok && index >= 0 && index < len(style.Colors.ANSI) {
						style.Colors.ANSI[index] = color
					}
				}
			}
		}
	}
	return style
}

func konsoleConfigPaths() []string {
	paths := []string{
		joinEnv("XDG_CONFIG_HOME", "konsolerc"),
		joinHome(".config", "konsolerc"),
	}
	paths = append(paths, globPaths(joinEnv("XDG_DATA_HOME", "konsole", "*.profile"))...)
	paths = append(paths, globPaths(joinHome(".local", "share", "konsole", "*.profile"))...)
	paths = append(paths, globPaths(joinEnv("XDG_DATA_HOME", "konsole", "*.colorscheme"))...)
	paths = append(paths, globPaths(joinHome(".local", "share", "konsole", "*.colorscheme"))...)
	return existingPaths(paths)
}

func parseKonsoleStyle(paths []string) terminalStyle {
	var style terminalStyle
	defaultProfile := os.Getenv("KONSOLE_PROFILE_NAME")
	profiles := map[string]string{}
	colorSchemes := map[string]string{}

	for _, path := range paths {
		base := filepath.Base(path)
		switch {
		case base == "konsolerc":
			if defaultProfile == "" {
				defaultProfile = parseKonsoleDefaultProfile(path)
			}
		case strings.HasSuffix(base, ".profile"):
			name := strings.TrimSuffix(base, ".profile")
			profiles[name] = path
		case strings.HasSuffix(base, ".colorscheme"):
			name := strings.TrimSuffix(base, ".colorscheme")
			colorSchemes[name] = path
		}
	}

	profilePath := ""
	if defaultProfile != "" {
		profilePath = profiles[strings.TrimSuffix(defaultProfile, ".profile")]
	}
	if profilePath == "" {
		for _, path := range profiles {
			profilePath = path
			break
		}
	}
	if profilePath != "" {
		profileStyle, schemeName := parseKonsoleProfile(profilePath)
		style.merge(profileStyle)
		if schemeName != "" {
			if schemePath := colorSchemes[schemeName]; schemePath != "" {
				style.merge(parseKonsoleColorScheme(schemePath))
			}
		}
	}
	return style
}

func windowsTerminalConfigPaths() []string {
	return existingPaths([]string{
		joinEnv("LOCALAPPDATA", "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		joinEnv("LOCALAPPDATA", "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json"),
		joinEnv("LOCALAPPDATA", "Microsoft", "Windows Terminal", "settings.json"),
	})
}

func parseWindowsTerminalStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		style.merge(parseWindowsTerminalConfig(string(data)))
	}
	return style
}

func parseWindowsTerminalConfig(data string) terminalStyle {
	var root map[string]any
	if err := json.Unmarshal([]byte(jsoncToJSON(data)), &root); err != nil {
		return terminalStyle{}
	}
	profiles, _ := root["profiles"].(map[string]any)
	defaults, _ := profiles["defaults"].(map[string]any)
	defaultProfileGUID, _ := root["defaultProfile"].(string)
	selected := findWindowsTerminalProfile(profiles, defaultProfileGUID)

	var style terminalStyle
	style.FontFamily = firstString(nestedString(selected, "font", "face"), nestedString(defaults, "font", "face"), stringValue(selected["fontFace"]), stringValue(defaults["fontFace"]))
	style.FontSize = firstFloat(nestedFloat(selected, "font", "size"), nestedFloat(defaults, "font", "size"), floatValue(selected["fontSize"]), floatValue(defaults["fontSize"]))
	schemeName := firstString(stringValue(selected["colorScheme"]), stringValue(defaults["colorScheme"]))
	style.Theme = themeFromName(schemeName)
	if schemeName != "" {
		style.Colors = windowsTerminalSchemeColors(root["schemes"], schemeName)
	}
	return style
}

func vscodeConfigPaths() []string {
	return existingPaths([]string{
		joinEnv("APPDATA", "Code", "User", "settings.json"),
		joinEnv("APPDATA", "Code - Insiders", "User", "settings.json"),
		joinHome("Library", "Application Support", "Code", "User", "settings.json"),
		joinHome("Library", "Application Support", "Code - Insiders", "User", "settings.json"),
		joinEnv("XDG_CONFIG_HOME", "Code", "User", "settings.json"),
		joinEnv("XDG_CONFIG_HOME", "Code - Insiders", "User", "settings.json"),
		joinHome(".config", "Code", "User", "settings.json"),
		joinHome(".config", "Code - Insiders", "User", "settings.json"),
	})
}

func parseVSCodeStyle(paths []string) terminalStyle {
	var style terminalStyle
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(jsoncToJSON(string(data))), &settings); err != nil {
			continue
		}
		var next terminalStyle
		next.FontFamily = stringValue(settings["terminal.integrated.fontFamily"])
		next.FontSize = floatValue(settings["terminal.integrated.fontSize"])
		next.Theme = themeFromName(stringValue(settings["workbench.colorTheme"]))
		style.merge(next)
	}
	return style
}

func parseAssignmentFile(path string) (map[string]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	assignments := map[string]string{}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripConfigComment(rawLine))
		if line == "" {
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		assignments[strings.ToLower(key)] = unquoteConfigValue(value)
	}
	return assignments, true
}

func splitKeyValue(line string) (string, string, bool) {
	if idx := strings.IndexAny(line, "=:"); idx >= 0 {
		return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.TrimSpace(strings.TrimPrefix(line, fields[0])), true
}

func stripConfigComment(line string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if b == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble {
			if b == '#' {
				return line[:i]
			}
			if b == '/' && i+1 < len(line) && line[i+1] == '/' {
				return line[:i]
			}
		}
	}
	return line
}

func unquoteConfigValue(value string) string {
	value = strings.TrimSpace(strings.TrimRight(value, ","))
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func parseHexColor(value string) string {
	value = strings.TrimSpace(unquoteConfigValue(value))
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

func parsePositiveFloat(value string) float64 {
	value = strings.TrimSpace(unquoteConfigValue(value))
	if value == "" {
		return 0
	}
	value = strings.TrimSuffix(value, ",")
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f
}

func parseIndexedColor(key, value string) (int, string, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if strings.Contains(value, "=") {
		parts := strings.SplitN(value, "=", 2)
		key = parts[0]
		value = parts[1]
	}
	index := -1
	for _, prefix := range []string{"color", "palette", "regular", "bright"} {
		if strings.HasPrefix(key, prefix) {
			idx, err := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(key, prefix), "-_. "))
			if err == nil {
				index = idx
				if prefix == "bright" && index < 8 {
					index += 8
				}
				break
			}
		}
	}
	color := parseHexColor(value)
	return index, color, index >= 0 && color != ""
}

func applyPathStyleValue(style *terminalStyle, path, value string) {
	path = strings.ToLower(strings.ReplaceAll(path, "_", "."))
	value = strings.TrimSpace(value)
	switch path {
	case "font.size":
		style.FontSize = parsePositiveFloat(value)
	case "font.normal.family", "font.family":
		style.FontFamily = unquoteConfigValue(value)
	case "colors.primary.background", "colors.background":
		style.Colors.Background = parseHexColor(value)
	case "colors.primary.foreground", "colors.foreground":
		style.Colors.Foreground = parseHexColor(value)
	default:
		applyNamedANSIColor(style, path, value)
	}
}

func applyNamedANSIColor(style *terminalStyle, path, value string) {
	parts := strings.Split(path, ".")
	if len(parts) < 3 || parts[0] != "colors" {
		return
	}
	paletteName := parts[len(parts)-2]
	colorName := parts[len(parts)-1]
	base := map[string]int{"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "purple": 5, "cyan": 6, "white": 7}
	index, ok := base[colorName]
	if !ok {
		return
	}
	if paletteName == "bright" || paletteName == "brights" {
		index += 8
	}
	if paletteName != "normal" && paletteName != "bright" && paletteName != "brights" {
		return
	}
	if color := parseHexColor(value); color != "" {
		style.Colors.ANSI[index] = color
	}
}

type yamlStackEntry struct {
	indent int
	name   string
}

func pushYamlStack(stack []yamlStackEntry, indent int, name string) []yamlStackEntry {
	stack = popYamlStack(stack, indent)
	return append(stack, yamlStackEntry{indent: indent, name: strings.ToLower(strings.TrimSpace(name))})
}

func popYamlStack(stack []yamlStackEntry, indent int) []yamlStackEntry {
	for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
		stack = stack[:len(stack)-1]
	}
	return stack
}

func yamlStackNames(stack []yamlStackEntry) []string {
	names := make([]string, len(stack))
	for i, entry := range stack {
		names[i] = entry.name
	}
	return names
}

func parseFootFont(value string) (string, float64) {
	parts := strings.Split(unquoteConfigValue(value), ":")
	family := strings.TrimSpace(parts[0])
	var size float64
	for _, part := range parts[1:] {
		key, val, ok := splitKeyValue(part)
		if ok && strings.EqualFold(key, "size") {
			size = parsePositiveFloat(val)
		}
	}
	return family, size
}

func parseKonsoleDefaultProfile(path string) string {
	assignments, ok := parseAssignmentFile(path)
	if !ok {
		return ""
	}
	return assignments["defaultprofile"]
}

func parseKonsoleProfile(path string) (terminalStyle, string) {
	assignments, ok := parseAssignmentFile(path)
	if !ok {
		return terminalStyle{}, ""
	}
	var style terminalStyle
	if font := assignments["font"]; font != "" {
		parts := strings.Split(font, ",")
		style.FontFamily = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			style.FontSize = parsePositiveFloat(parts[1])
		}
	}
	scheme := assignments["colorscheme"]
	style.Theme = themeFromName(scheme)
	return style, scheme
}

func parseKonsoleColorScheme(path string) terminalStyle {
	data, err := os.ReadFile(path)
	if err != nil {
		return terminalStyle{}
	}
	var style terminalStyle
	section := ""
	sectionIndex := map[string]int{"color0": 0, "color1": 1, "color2": 2, "color3": 3, "color4": 4, "color5": 5, "color6": 6, "color7": 7, "color0intense": 8, "color1intense": 9, "color2intense": 10, "color3intense": 11, "color4intense": 12, "color5intense": 13, "color6intense": 14, "color7intense": 15}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripConfigComment(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1:strings.Index(line, "]")]))
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok || strings.ToLower(key) != "color" {
			continue
		}
		color := parseRGBTriplet(value)
		if color == "" {
			continue
		}
		if section == "general" {
			continue
		}
		if idx, ok := sectionIndex[section]; ok {
			style.Colors.ANSI[idx] = color
		}
	}
	return style
}

func parseRGBTriplet(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return ""
	}
	var rgb [3]int
	for i, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || v < 0 || v > 255 {
			return ""
		}
		rgb[i] = v
	}
	return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
}

func findWindowsTerminalProfile(profiles map[string]any, guid string) map[string]any {
	list, _ := profiles["list"].([]any)
	var first map[string]any
	for _, item := range list {
		profile, _ := item.(map[string]any)
		if profile == nil {
			continue
		}
		if first == nil {
			first = profile
		}
		if guid != "" && strings.EqualFold(stringValue(profile["guid"]), guid) {
			return profile
		}
	}
	return first
}

func windowsTerminalSchemeColors(schemes any, name string) svg.Colors {
	list, _ := schemes.([]any)
	for _, item := range list {
		scheme, _ := item.(map[string]any)
		if scheme == nil || !strings.EqualFold(stringValue(scheme["name"]), name) {
			continue
		}
		var colors svg.Colors
		colors.Background = parseHexColor(stringValue(scheme["background"]))
		colors.Foreground = parseHexColor(stringValue(scheme["foreground"]))
		keys := []string{"black", "red", "green", "yellow", "blue", "purple", "cyan", "white", "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightPurple", "brightCyan", "brightWhite"}
		for i, key := range keys {
			colors.ANSI[i] = parseHexColor(stringValue(scheme[key]))
		}
		return colors
	}
	return svg.Colors{}
}

func nestedString(m map[string]any, path ...string) string {
	var current any = m
	for _, key := range path {
		next, _ := current.(map[string]any)
		if next == nil {
			return ""
		}
		current = next[key]
	}
	return stringValue(current)
}

func nestedFloat(m map[string]any, path ...string) float64 {
	var current any = m
	for _, key := range path {
		next, _ := current.(map[string]any)
		if next == nil {
			return 0
		}
		current = next[key]
	}
	return floatValue(current)
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func floatValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		return parsePositiveFloat(v)
	default:
		return 0
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func jsoncToJSON(data string) string {
	data = stripJSONComments(data)
	return regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(data, `$1`)
}

func stripJSONComments(data string) string {
	var b strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			b.WriteByte(c)
			inString = !inString
			continue
		}
		if !inString && c == '/' && i+1 < len(data) {
			if data[i+1] == '/' {
				for i < len(data) && data[i] != '\n' {
					i++
				}
				if i < len(data) {
					b.WriteByte(data[i])
				}
				continue
			}
			if data[i+1] == '*' {
				i += 2
				for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func quotedStrings(data string) []string {
	re := regexp.MustCompile(`["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(data, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, match[1])
	}
	return values
}

func quotedHexColors(data string) []string {
	var colors []string
	for _, value := range quotedStrings(data) {
		if color := parseHexColor(value); color != "" {
			colors = append(colors, color)
		}
	}
	return colors
}

func firstHexAssignment(data, key string) string {
	re := regexp.MustCompile(`(?m)` + regexp.QuoteMeta(key) + `\s*=\s*["']?([^"'\s,}]+)`)
	if m := re.FindStringSubmatch(data); len(m) == 2 {
		return parseHexColor(m[1])
	}
	return ""
}

func splitCSV(value string) []string {
	var parts []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	for _, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteRune(r)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteRune(r)
		case ',':
			if inSingle || inDouble {
				b.WriteRune(r)
			} else {
				parts = append(parts, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	parts = append(parts, b.String())
	return parts
}

func expandConfigPathList(value, base string) []string {
	var paths []string
	for _, raw := range splitCSV(value) {
		path := expandPath(strings.TrimSpace(unquoteConfigValue(raw)), base)
		if strings.ContainsAny(path, "*?[{") {
			paths = append(paths, globPaths(path)...)
		} else if path != "" {
			paths = append(paths, path)
		}
	}
	return existingPaths(paths)
}

func expandPath(path, base string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	path = os.ExpandEnv(path)
	if !filepath.IsAbs(path) && base != "" {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

func globPaths(pattern string) []string {
	if pattern == "" {
		return nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func existingPaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = expandPath(path, "")
		if path == "" || seen[path] {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func joinHome(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	items := append([]string{home}, parts...)
	return filepath.Join(items...)
}

func joinEnv(name string, parts ...string) string {
	base := os.Getenv(name)
	if base == "" && name == "XDG_CONFIG_HOME" {
		return joinHome(append([]string{".config"}, parts...)...)
	}
	if base == "" && name == "XDG_DATA_HOME" {
		return joinHome(append([]string{".local", "share"}, parts...)...)
	}
	if base == "" {
		return ""
	}
	items := append([]string{base}, parts...)
	return filepath.Join(items...)
}

func themeFromName(name string) string {
	name = strings.ToLower(name)
	if name == "" {
		return ""
	}
	lightMarkers := []string{"light", "day", "latte", "dawn", "modus operandi", "paper", "peachpuff"}
	for _, marker := range lightMarkers {
		if strings.Contains(name, marker) {
			return "light"
		}
	}
	darkMarkers := []string{"dark", "night", "moon", "mocha", "macchiato", "frappe", "nord", "dracula", "onedark", "one dark", "tokyo", "kanagawa", "gruvbox", "catppuccin"}
	for _, marker := range darkMarkers {
		if strings.Contains(name, marker) {
			return "dark"
		}
	}
	return ""
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
