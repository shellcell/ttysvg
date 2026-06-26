package terminal

import (
	"bytes"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Terminfo struct {
	fixed []terminfoFixed
	cup   *terminfoCursorPattern
}

type terminfoFixed struct {
	seq    []byte
	action terminfoAction
}

type terminfoAction uint8

const (
	terminfoClear terminfoAction = iota
	terminfoEraseDisplay
	terminfoEraseLine
	terminfoEraseLineLeft
	terminfoEnterAlt
	terminfoExitAlt
	terminfoCursorInvisible
	terminfoCursorNormal
	terminfoSaveCursor
	terminfoRestoreCursor
	terminfoHome
	terminfoLastLine
)

type terminfoCursorPattern struct {
	re        *regexp.Regexp
	params    []int
	increment bool
}

func LoadTerminfo(term string) (Terminfo, bool) {
	if term == "" {
		return Terminfo{}, false
	}
	if info, ok := builtinTerminfo(term); ok {
		return info, true
	}
	out, err := exec.Command("infocmp", "-1", "-x", term).Output()
	if err != nil {
		return Terminfo{}, false
	}
	return ParseTerminfo(out)
}

func builtinTerminfo(term string) (Terminfo, bool) {
	name := strings.ToLower(term)
	if !(strings.HasPrefix(name, "xterm") || strings.HasPrefix(name, "tmux") || strings.HasPrefix(name, "screen") || strings.HasPrefix(name, "rxvt") || strings.Contains(name, "kitty") || strings.Contains(name, "ghostty") || strings.Contains(name, "wezterm") || strings.Contains(name, "alacritty")) {
		return Terminfo{}, false
	}
	return Terminfo{
		fixed: []terminfoFixed{
			{seq: []byte("\x1b[H\x1b[2J"), action: terminfoClear},
			{seq: []byte("\x1b[?1049h"), action: terminfoEnterAlt},
			{seq: []byte("\x1b[?1049l"), action: terminfoExitAlt},
			{seq: []byte("\x1b[?25l"), action: terminfoCursorInvisible},
			{seq: []byte("\x1b[?25h"), action: terminfoCursorNormal},
			{seq: []byte("\x1b[1K"), action: terminfoEraseLineLeft},
			{seq: []byte("\x1b[J"), action: terminfoEraseDisplay},
			{seq: []byte("\x1b[K"), action: terminfoEraseLine},
			{seq: []byte("\x1b7"), action: terminfoSaveCursor},
			{seq: []byte("\x1b8"), action: terminfoRestoreCursor},
			{seq: []byte("\x1b[H"), action: terminfoHome},
		},
		cup: compileTerminfoCursorPattern("\x1b[%i%p1%d;%p2%dH"),
	}, true
}

func ParseTerminfo(data []byte) (Terminfo, bool) {
	caps := parseInfocmpCapabilities(data)
	var info Terminfo
	for name, action := range map[string]terminfoAction{
		"clear": terminfoClear,
		"ed":    terminfoEraseDisplay,
		"el":    terminfoEraseLine,
		"el1":   terminfoEraseLineLeft,
		"smcup": terminfoEnterAlt,
		"rmcup": terminfoExitAlt,
		"civis": terminfoCursorInvisible,
		"cnorm": terminfoCursorNormal,
		"sc":    terminfoSaveCursor,
		"rc":    terminfoRestoreCursor,
		"home":  terminfoHome,
		"ll":    terminfoLastLine,
	} {
		seq := caps[name]
		if seq == "" || strings.Contains(seq, "%") {
			continue
		}
		info.fixed = append(info.fixed, terminfoFixed{seq: []byte(seq), action: action})
	}
	if cup := caps["cup"]; cup != "" {
		info.cup = compileTerminfoCursorPattern(cup)
	}
	sort.Slice(info.fixed, func(i, j int) bool {
		return len(info.fixed[i].seq) > len(info.fixed[j].seq)
	})
	return info, len(info.fixed) > 0 || info.cup != nil
}

func parseInfocmpCapabilities(data []byte) map[string]string {
	caps := map[string]string{}
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(raw))
		line = strings.TrimSuffix(line, ",")
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		name := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if name != "" {
			caps[name] = unescapeTerminfoString(value)
		}
	}
	return caps
}

func unescapeTerminfoString(value string) string {
	var out []byte
	for i := 0; i < len(value); i++ {
		b := value[i]
		switch b {
		case '^':
			if i+1 < len(value) {
				i++
				next := value[i]
				if next == '?' {
					out = append(out, 0x7f)
				} else {
					out = append(out, next&0x1f)
				}
			} else {
				out = append(out, b)
			}
		case '\\':
			if i+1 >= len(value) {
				out = append(out, b)
				continue
			}
			i++
			next := value[i]
			switch next {
			case 'E', 'e':
				out = append(out, 0x1b)
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 's':
				out = append(out, ' ')
			case '\\', ',', ':', '^':
				out = append(out, next)
			default:
				if next >= '0' && next <= '7' {
					octal := []byte{next}
					for len(octal) < 3 && i+1 < len(value) && value[i+1] >= '0' && value[i+1] <= '7' {
						i++
						octal = append(octal, value[i])
					}
					v, err := strconv.ParseUint(string(octal), 8, 8)
					if err == nil {
						out = append(out, byte(v))
					}
				} else {
					out = append(out, next)
				}
			}
		default:
			out = append(out, b)
		}
	}
	return string(out)
}

func compileTerminfoCursorPattern(template string) *terminfoCursorPattern {
	increment := strings.Contains(template, "%i")
	template = strings.ReplaceAll(template, "%i", "")
	var params []int
	var re strings.Builder
	for i := 0; i < len(template); {
		if strings.HasPrefix(template[i:], "%p1%d") || strings.HasPrefix(template[i:], "%p1%2") || strings.HasPrefix(template[i:], "%p1%3") || strings.HasPrefix(template[i:], "%p1%02d") || strings.HasPrefix(template[i:], "%p1%03d") {
			params = append(params, 1)
			re.WriteString(`([0-9]+)`)
			i += terminfoParamTokenLen(template[i:])
			continue
		}
		if strings.HasPrefix(template[i:], "%p2%d") || strings.HasPrefix(template[i:], "%p2%2") || strings.HasPrefix(template[i:], "%p2%3") || strings.HasPrefix(template[i:], "%p2%02d") || strings.HasPrefix(template[i:], "%p2%03d") {
			params = append(params, 2)
			re.WriteString(`([0-9]+)`)
			i += terminfoParamTokenLen(template[i:])
			continue
		}
		re.WriteString(regexp.QuoteMeta(template[i : i+1]))
		i++
	}
	if len(params) != 2 {
		return nil
	}
	compiled, err := regexp.Compile("^" + re.String())
	if err != nil {
		return nil
	}
	return &terminfoCursorPattern{re: compiled, params: params, increment: increment}
}

func terminfoParamTokenLen(s string) int {
	for _, token := range []string{"%p1%03d", "%p2%03d", "%p1%02d", "%p2%02d", "%p1%d", "%p2%d", "%p1%2", "%p2%2", "%p1%3", "%p2%3"} {
		if strings.HasPrefix(s, token) {
			return len(token)
		}
	}
	return 0
}

func (p *terminfoCursorPattern) match(data []byte) (int, int, int, bool) {
	if p == nil {
		return 0, 0, 0, false
	}
	matches := p.re.FindSubmatchIndex(data)
	if len(matches) != 6 || matches[0] != 0 {
		return 0, 0, 0, false
	}
	values := map[int]int{}
	for group, param := range p.params {
		start := matches[2+group*2]
		end := matches[3+group*2]
		value, err := strconv.Atoi(string(data[start:end]))
		if err != nil {
			return 0, 0, 0, false
		}
		if p.increment {
			value--
		}
		values[param] = value
	}
	return matches[1], values[2], values[1], true
}
