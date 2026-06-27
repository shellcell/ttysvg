package terminal

import (
	"bytes"
	"os/exec"
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

// terminfoCursorPattern matches a cursor-address (cup) sequence at the start of
// a byte stream. The cup capability is a template of literal bytes interleaved
// with two numeric parameters (row and column), so it is represented as an
// ordered list of literal/number items and matched by hand, avoiding a regexp
// dependency.
type terminfoCursorPattern struct {
	items     []cupItem
	params    []int
	increment bool
}

type cupItem struct {
	literal string // literal bytes to match exactly (when isNum is false)
	isNum   bool   // true for a numeric parameter capture
	param   int    // parameter id (1 or 2) for a numeric item
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
	var items []cupItem
	var params []int
	var lit strings.Builder
	flushLit := func() {
		if lit.Len() > 0 {
			items = append(items, cupItem{literal: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(template); {
		if n := terminfoParamTokenLen(template[i:]); n > 0 {
			param := 2
			if strings.HasPrefix(template[i:], "%p1") {
				param = 1
			}
			flushLit()
			items = append(items, cupItem{isNum: true, param: param})
			params = append(params, param)
			i += n
			continue
		}
		lit.WriteByte(template[i])
		i++
	}
	flushLit()
	if len(params) != 2 {
		return nil
	}
	return &terminfoCursorPattern{items: items, params: params, increment: increment}
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
	pos := 0
	values := map[int]int{}
	for _, item := range p.items {
		if !item.isNum {
			if !bytes.HasPrefix(data[pos:], []byte(item.literal)) {
				return 0, 0, 0, false
			}
			pos += len(item.literal)
			continue
		}
		start := pos
		for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
			pos++
		}
		if pos == start {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(string(data[start:pos]))
		if err != nil {
			return 0, 0, 0, false
		}
		if p.increment {
			value--
		}
		values[item.param] = value
	}
	return pos, values[2], values[1], true
}
