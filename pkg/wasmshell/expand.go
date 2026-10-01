package wasmshell

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type wordPart struct {
	text   string
	quoted bool
}

type expandMode int

const (
	modeFields  expandMode = iota // split and glob: command words
	modeString                    // one string: assignments, redirect targets
	modeHeredoc                   // quotes are literal text
)

type wordExpander struct {
	sh     *interp
	mode   expandMode
	fields [][]wordPart
	cur    []wordPart
	has    bool
}

func (w *wordExpander) add(text string, quoted bool) {
	w.cur = append(w.cur, wordPart{text, quoted})
	w.has = true
}

func (w *wordExpander) endField() {
	if w.has {
		w.fields = append(w.fields, w.cur)
	}
	w.cur, w.has = nil, false
}

// addExpansion adds the result of an unquoted expansion, which undergoes
// field splitting in command words.
func (w *wordExpander) addExpansion(val string) {
	if w.mode != modeFields {
		w.add(val, false)
		return
	}
	ifs := w.sh.ifs()
	start := 0
	for i, r := range val {
		if strings.ContainsRune(ifs, r) {
			if i > start {
				w.add(val[start:i], false)
			}
			w.endField()
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(val) {
		w.add(val[start:], false)
	}
}

func (sh *interp) ifs() string {
	if v, ok := sh.lookup("IFS"); ok {
		return v
	}
	return " \t\n"
}

func (sh *interp) expandFields(raw string) ([]string, error) {
	var out []string
	for _, word := range braceExpand(raw) {
		w := &wordExpander{sh: sh, mode: modeFields}
		if err := w.walk(word); err != nil {
			return nil, err
		}
		w.endField()
		for _, f := range w.fields {
			out = append(out, globParts(f)...)
		}
	}
	return out, nil
}

func (sh *interp) expandString(raw string) (string, error) {
	return sh.expandMode(raw, modeString)
}

func (sh *interp) expandMode(raw string, mode expandMode) (string, error) {
	w := &wordExpander{sh: sh, mode: mode}
	if err := w.walk(raw); err != nil {
		return "", err
	}
	w.endField()
	var b strings.Builder
	for i, f := range w.fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		for _, p := range f {
			b.WriteString(p.text)
		}
	}
	return b.String(), nil
}

// expandPattern expands a case or [[ == ]] pattern into an anchored
// regexp: quoted parts match literally, unquoted glob characters don't.
func (sh *interp) expandPattern(raw string) (*regexp.Regexp, error) {
	w := &wordExpander{sh: sh, mode: modeString}
	if err := w.walk(raw); err != nil {
		return nil, err
	}
	w.endField()
	var b strings.Builder
	b.WriteString("^(?s:")
	for _, f := range w.fields {
		for _, p := range f {
			if p.quoted {
				b.WriteString(regexp.QuoteMeta(p.text))
			} else {
				b.WriteString(globToRegexp(p.text))
			}
		}
	}
	b.WriteString(")$")
	return regexp.Compile(b.String())
}

func (w *wordExpander) walk(raw string) error {
	rs := []rune(raw)
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case c == '\\' && w.mode == modeHeredoc:
			if i+1 < len(rs) && strings.ContainsRune("$`\\\n", rs[i+1]) {
				if rs[i+1] != '\n' {
					w.add(string(rs[i+1]), true)
				}
				i += 2
				continue
			}
			w.add("\\", true)
			i++
		case c == '\\':
			if i+1 < len(rs) {
				if rs[i+1] != '\n' {
					w.add(string(rs[i+1]), true)
				}
				i += 2
				continue
			}
			w.add("\\", true)
			i++
		case c == '\'' && w.mode != modeHeredoc:
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			w.add(string(rs[i+1:min(j, len(rs))]), true)
			i = j + 1
		case c == '"' && w.mode != modeHeredoc:
			w.has = true
			next, err := w.walkDouble(rs, i+1)
			if err != nil {
				return err
			}
			i = next
		case c == '$':
			next, err := w.dollar(rs, i, false)
			if err != nil {
				return err
			}
			i = next
		case c == '`':
			next, err := w.backtick(rs, i, false)
			if err != nil {
				return err
			}
			i = next
		case c == '~' && i == 0 && w.mode != modeHeredoc && (len(rs) == 1 || rs[1] == '/'):
			w.add(ShellEnv.Get("HOME"), true)
			i++
		default:
			w.add(string(c), w.mode == modeHeredoc)
			i++
		}
	}
	return nil
}

func (w *wordExpander) walkDouble(rs []rune, i int) (int, error) {
	for i < len(rs) && rs[i] != '"' {
		c := rs[i]
		switch {
		case c == '\\' && i+1 < len(rs) && strings.ContainsRune("$`\"\\\n", rs[i+1]):
			if rs[i+1] != '\n' {
				w.add(string(rs[i+1]), true)
			}
			i += 2
		case c == '$':
			next, err := w.dollar(rs, i, true)
			if err != nil {
				return 0, err
			}
			i = next
		case c == '`':
			next, err := w.backtick(rs, i, true)
			if err != nil {
				return 0, err
			}
			i = next
		default:
			w.add(string(c), true)
			i++
		}
	}
	return i + 1, nil
}

func (w *wordExpander) emit(val string, quoted bool) {
	if quoted {
		w.add(val, true)
	} else {
		w.addExpansion(val)
	}
}

func matchClose(rs []rune, i int, open, close rune) (int, error) {
	lx := &lexer{src: rs, pos: i}
	if err := lx.skipBalanced(open, close); err != nil {
		return 0, err
	}
	return lx.pos, nil
}

func (w *wordExpander) dollar(rs []rune, i int, quoted bool) (int, error) {
	if i+1 >= len(rs) {
		w.add("$", quoted)
		return i + 1, nil
	}
	c := rs[i+1]
	switch {
	case c == '(' && i+2 < len(rs) && rs[i+2] == '(':
		end, err := matchClose(rs, i+1, '(', ')')
		if err != nil {
			return 0, err
		}
		inner := string(rs[i+3 : end-2])
		n, err := w.sh.arith(inner)
		if err != nil {
			return 0, err
		}
		w.emit(strconv.FormatInt(n, 10), quoted)
		return end, nil
	case c == '(':
		end, err := matchClose(rs, i+1, '(', ')')
		if err != nil {
			return 0, err
		}
		w.emit(w.sh.substitute(string(rs[i+2:end-1])), quoted)
		return end, nil
	case c == '{':
		end, err := matchClose(rs, i+1, '{', '}')
		if err != nil {
			return 0, err
		}
		inner := string(rs[i+2 : end-1])
		if inner == "@" && quoted {
			w.addAt()
			return end, nil
		}
		val, err := w.sh.paramExpand(inner)
		if err != nil {
			return 0, err
		}
		w.emit(val, quoted)
		return end, nil
	case c == '\'' && !quoted && w.mode != modeHeredoc:
		j := i + 2
		for j < len(rs) && rs[j] != '\'' {
			if rs[j] == '\\' {
				j++
			}
			j++
		}
		w.add(unescapeC(string(rs[i+2:min(j, len(rs))]), false), true)
		return j + 1, nil
	case c == '@' && quoted:
		w.addAt()
		return i + 2, nil
	case strings.ContainsRune("?#@*$!-0123456789", c):
		w.emit(w.sh.special(string(c)), quoted)
		return i + 2, nil
	case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		j := i + 1
		for j < len(rs) && (rs[j] == '_' || (rs[j] >= 'a' && rs[j] <= 'z') || (rs[j] >= 'A' && rs[j] <= 'Z') || (rs[j] >= '0' && rs[j] <= '9')) {
			j++
		}
		name := string(rs[i+1 : j])
		val, set := w.sh.lookup(name)
		if !set && w.sh.nounset {
			return 0, fmt.Errorf("%s: unbound variable", name)
		}
		w.emit(val, quoted)
		return j, nil
	}
	w.add("$", quoted)
	return i + 1, nil
}

// addAt expands a quoted "$@": one field per positional parameter.
func (w *wordExpander) addAt() {
	params := w.sh.positional
	if len(params) == 0 {
		return
	}
	for k, p := range params {
		if k > 0 {
			w.endField()
		}
		w.add(p, true)
	}
}

func (w *wordExpander) backtick(rs []rune, i int, quoted bool) (int, error) {
	var b strings.Builder
	j := i + 1
	for j < len(rs) && rs[j] != '`' {
		if rs[j] == '\\' && j+1 < len(rs) && strings.ContainsRune("`$\\", rs[j+1]) {
			j++
		}
		b.WriteRune(rs[j])
		j++
	}
	if j >= len(rs) {
		return 0, errSyntax("unterminated `")
	}
	w.emit(w.sh.substitute(b.String()), quoted)
	return j + 1, nil
}
