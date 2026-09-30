package wasmshell

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type sedParser struct {
	src  []rune
	pos  int
	prog *sedProg
}

func (p *sedProg) parse(script string) ([]*sedCmd, error) {
	sp := &sedParser{src: []rune(script), prog: p}
	return sp.cmds(false)
}

func (sp *sedParser) peek() rune {
	if sp.pos < len(sp.src) {
		return sp.src[sp.pos]
	}
	return 0
}

func (sp *sedParser) skipSpaces() {
	for sp.pos < len(sp.src) && (sp.src[sp.pos] == ' ' || sp.src[sp.pos] == '\t') {
		sp.pos++
	}
}

func (sp *sedParser) cmds(inBlock bool) ([]*sedCmd, error) {
	var out []*sedCmd
	for {
		for sp.pos < len(sp.src) && strings.ContainsRune(" \t\n;", sp.src[sp.pos]) {
			sp.pos++
		}
		if sp.pos >= len(sp.src) {
			if inBlock {
				return nil, fmt.Errorf("unmatched `{'")
			}
			return out, nil
		}
		switch sp.peek() {
		case '}':
			if !inBlock {
				return nil, fmt.Errorf("unexpected `}'")
			}
			sp.pos++
			return out, nil
		case '#':
			for sp.pos < len(sp.src) && sp.src[sp.pos] != '\n' {
				sp.pos++
			}
			continue
		}
		c, err := sp.command()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
}

func (sp *sedParser) command() (*sedCmd, error) {
	c := &sedCmd{}
	var err error
	if c.a1, err = sp.addr(false); err != nil {
		return nil, err
	}
	sp.skipSpaces()
	if c.a1 != nil && sp.peek() == ',' {
		sp.pos++
		sp.skipSpaces()
		if c.a2, err = sp.addr(true); err != nil {
			return nil, err
		}
		if c.a2 == nil {
			return nil, fmt.Errorf("unexpected `,'")
		}
	}
	sp.skipSpaces()
	for sp.peek() == '!' {
		c.negate = true
		sp.pos++
		sp.skipSpaces()
	}
	if sp.pos >= len(sp.src) {
		return nil, fmt.Errorf("missing command")
	}
	if sp.src[sp.pos] > 0x7f {
		return nil, fmt.Errorf("unknown command: `%c'", sp.src[sp.pos])
	}
	c.name = byte(sp.src[sp.pos]) //nolint:gosec // G115: guarded to ASCII just above
	sp.pos++
	switch c.name {
	case '{':
		if c.block, err = sp.cmds(true); err != nil {
			return nil, err
		}
		return c, nil
	case 's':
		err = sp.subst(c)
	case 'y':
		err = sp.translit(c)
	case 'a', 'i', 'c':
		c.text = sp.text()
		return c, nil
	case 'q', 'Q':
		sp.skipSpaces()
		start := sp.pos
		for sp.pos < len(sp.src) && sp.src[sp.pos] >= '0' && sp.src[sp.pos] <= '9' {
			sp.pos++
		}
		c.exitCode, _ = strconv.Atoi(string(sp.src[start:sp.pos]))
	case ':':
		sp.skipSpaces()
		c.label = sp.label()
		if c.label == "" {
			return nil, fmt.Errorf("\":\" lacks a label")
		}
	case 'b', 't', 'T':
		sp.skipSpaces()
		c.label = sp.label()
	case 'd', 'D', 'p', 'P', 'n', 'N', '=', 'h', 'H', 'g', 'G', 'x', 'l', 'z':
	case 'r', 'R', 'w', 'W', 'e', 'F', 'v':
		return nil, fmt.Errorf("command `%c' is not supported by the in-browser shell", c.name)
	default:
		return nil, fmt.Errorf("unknown command: `%c'", c.name)
	}
	if err != nil {
		return nil, err
	}
	sp.skipSpaces()
	if sp.pos < len(sp.src) && !strings.ContainsRune(";\n}#", sp.src[sp.pos]) {
		return nil, fmt.Errorf("extra characters after command")
	}
	return c, nil
}

func (sp *sedParser) label() string {
	start := sp.pos
	for sp.pos < len(sp.src) && !strings.ContainsRune(";\n}", sp.src[sp.pos]) {
		sp.pos++
	}
	return strings.TrimSpace(string(sp.src[start:sp.pos]))
}

// text reads the argument of a/i/c: GNU one-line form ("a text") or the
// POSIX form ("a\" newline text), with backslash-newline continuing lines.
func (sp *sedParser) text() string {
	if sp.peek() == '\\' {
		sp.pos++
		if sp.peek() == '\n' {
			sp.pos++
		}
	}
	sp.skipSpaces()
	var b strings.Builder
	for sp.pos < len(sp.src) {
		ch := sp.src[sp.pos]
		if ch == '\n' {
			sp.pos++
			break
		}
		if ch == '\\' && sp.pos+1 < len(sp.src) {
			sp.pos++
			n := sp.src[sp.pos]
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteRune(n)
			}
			sp.pos++
			continue
		}
		b.WriteRune(ch)
		sp.pos++
	}
	return b.String()
}

func (sp *sedParser) addr(second bool) (*sedAddr, error) {
	c := sp.peek()
	switch {
	case c >= '0' && c <= '9':
		n := sp.number()
		if sp.peek() == '~' {
			sp.pos++
			return &sedAddr{kind: '~', line: n, step: sp.number()}, nil
		}
		return &sedAddr{kind: 'n', line: n}, nil
	case c == '+' && second:
		sp.pos++
		return &sedAddr{kind: 'n', plusN: sp.number()}, nil
	case c == '$':
		sp.pos++
		return &sedAddr{kind: '$'}, nil
	case c == '/' || c == '\\':
		delim := c
		if c == '\\' {
			sp.pos++
			delim = sp.peek()
		}
		sp.pos++
		pat, err := sp.delimited(delim, true)
		if err != nil {
			return nil, err
		}
		flags := ""
		for sp.peek() == 'I' || sp.peek() == 'M' {
			flags += string(sp.peek())
			sp.pos++
		}
		re, err := sp.prog.compile(pat, flags)
		if err != nil {
			return nil, err
		}
		return &sedAddr{kind: '/', re: re}, nil
	}
	return nil, nil
}

func (sp *sedParser) number() int {
	start := sp.pos
	for sp.pos < len(sp.src) && sp.src[sp.pos] >= '0' && sp.src[sp.pos] <= '9' {
		sp.pos++
	}
	n, _ := strconv.Atoi(string(sp.src[start:sp.pos]))
	return n
}

// delimited reads up to an unescaped delimiter; an escaped delimiter
// becomes the literal character. In a regex the delimiter inside a
// bracket expression doesn't end it.
func (sp *sedParser) delimited(delim rune, regex bool) (string, error) {
	var b strings.Builder
	inBracket := false
	for sp.pos < len(sp.src) {
		ch := sp.src[sp.pos]
		switch {
		case ch == '\\' && sp.pos+1 < len(sp.src):
			n := sp.src[sp.pos+1]
			switch n {
			case delim:
				b.WriteRune(n)
			case '\n':
				b.WriteByte('\n')
			default:
				b.WriteRune(ch)
				b.WriteRune(n)
			}
			sp.pos += 2
			continue
		case regex && ch == '[' && !inBracket:
			inBracket = true
			b.WriteRune(ch)
			sp.pos++
			if sp.peek() == '^' {
				b.WriteRune('^')
				sp.pos++
			}
			if sp.peek() == ']' {
				b.WriteRune(']')
				sp.pos++
			}
			continue
		case regex && ch == ']' && inBracket:
			inBracket = false
		case ch == delim && !inBracket:
			sp.pos++
			return b.String(), nil
		}
		b.WriteRune(ch)
		sp.pos++
	}
	return "", fmt.Errorf("unterminated address regex or `s' command")
}

func (sp *sedParser) subst(c *sedCmd) error {
	if sp.pos >= len(sp.src) {
		return fmt.Errorf("unterminated `s' command")
	}
	delim := sp.src[sp.pos]
	sp.pos++
	pat, err := sp.delimited(delim, true)
	if err != nil {
		return err
	}
	repl, err := sp.delimited(delim, false)
	if err != nil {
		return fmt.Errorf("unterminated `s' command")
	}
	c.repl = repl
	flags := ""
	for sp.pos < len(sp.src) {
		ch := sp.src[sp.pos]
		switch {
		case ch == 'g':
			c.global = true
		case ch == 'p':
			c.print = true
		case ch == 'i' || ch == 'I' || ch == 'm' || ch == 'M':
			flags += strings.ToUpper(string(ch))
		case ch >= '0' && ch <= '9':
			c.nth = sp.number()
			continue
		case ch == 'w' || ch == 'e':
			return fmt.Errorf("s///%c is not supported by the in-browser shell", ch)
		default:
			c.re, err = sp.prog.compile(pat, flags)
			return err
		}
		sp.pos++
	}
	c.re, err = sp.prog.compile(pat, flags)
	return err
}

func (sp *sedParser) translit(c *sedCmd) error {
	if sp.pos >= len(sp.src) {
		return fmt.Errorf("unterminated `y' command")
	}
	delim := sp.src[sp.pos]
	sp.pos++
	from, err := sp.delimited(delim, false)
	if err != nil {
		return err
	}
	to, err := sp.delimited(delim, false)
	if err != nil {
		return err
	}
	c.from, c.to = []rune(unescapeC(from, false)), []rune(unescapeC(to, false))
	if len(c.from) != len(c.to) {
		return fmt.Errorf("strings for `y' command are different lengths")
	}
	return nil
}

// compile turns a sed regex into Go syntax; an empty regex reuses the
// last one, as sed does.
func (p *sedProg) compile(pat, flags string) (*regexp.Regexp, error) {
	if pat == "" {
		if p.lastRe == nil {
			return nil, fmt.Errorf("no previous regular expression")
		}
		return p.lastRe, nil
	}
	if !p.ere {
		pat = breToERE(pat)
	}
	pat = strings.NewReplacer(`\<`, `\b`, `\>`, `\b`, "\\`", `\A`, `\'`, `\z`).Replace(pat)
	prefix := ""
	if strings.Contains(flags, "I") {
		prefix += "i"
	}
	if strings.Contains(flags, "M") {
		prefix += "m"
	}
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %s", err)
	}
	p.lastRe = re
	return re, nil
}
