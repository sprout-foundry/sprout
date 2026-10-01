package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tWord
	tOp
	tNewline
	tRedir
	tArith
)

type token struct {
	kind tokKind
	text string
	fd   int
}

// lexer turns shell source into words, operators and redirections. Words
// keep their quoting; expansion happens at run time, word by word.
type lexer struct {
	src      []rune
	pos      int
	peeked   *token
	heredocs []*redir
}

func newLexer(src string) *lexer {
	return &lexer{src: []rune(src)}
}

func (lx *lexer) peek() (token, error) {
	if lx.peeked == nil {
		t, err := lx.lex()
		if err != nil {
			return token{}, err
		}
		lx.peeked = &t
	}
	return *lx.peeked, nil
}

func (lx *lexer) next() (token, error) {
	t, err := lx.peek()
	lx.peeked = nil
	return t, err
}

func (lx *lexer) at(i int) rune {
	if i < len(lx.src) {
		return lx.src[i]
	}
	return 0
}

func (lx *lexer) skipBlanks() {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			lx.pos++
			continue
		}
		if c == '\\' && lx.at(lx.pos+1) == '\n' {
			lx.pos += 2
			continue
		}
		if c == '#' {
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
			continue
		}
		return
	}
}

func isOpStart(c rune) bool {
	return c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '<' || c == '>'
}

func (lx *lexer) lex() (token, error) {
	lx.skipBlanks()
	if lx.pos >= len(lx.src) {
		return token{kind: tEOF}, nil
	}
	c := lx.src[lx.pos]
	if c == '\n' {
		lx.pos++
		if err := lx.readHeredocBodies(); err != nil {
			return token{}, err
		}
		return token{kind: tNewline, text: "\n"}, nil
	}

	// An fd number glued to a redirection: 2>, 2>>, 1>&2, 0<.
	if c >= '0' && c <= '9' {
		j := lx.pos
		for j < len(lx.src) && lx.src[j] >= '0' && lx.src[j] <= '9' {
			j++
		}
		if j < len(lx.src) && (lx.src[j] == '<' || lx.src[j] == '>') {
			fd, _ := strconv.Atoi(string(lx.src[lx.pos:j]))
			lx.pos = j
			op := lx.redirOp()
			return token{kind: tRedir, text: op, fd: fd}, nil
		}
	}

	switch {
	case c == '<' || c == '>':
		return token{kind: tRedir, text: lx.redirOp(), fd: -1}, nil
	case c == '&' && lx.at(lx.pos+1) == '>':
		lx.pos++
		op := lx.redirOp()
		return token{kind: tRedir, text: "&" + op, fd: -1}, nil
	case c == '(' && lx.at(lx.pos+1) == '(':
		body, ok := lx.scanArith()
		if ok {
			return token{kind: tArith, text: body}, nil
		}
	}
	if isOpStart(c) {
		for _, op := range []string{"&&", "||", ";;", "|&", ";", "&", "|", "(", ")"} {
			if lx.hasPrefix(op) {
				lx.pos += len([]rune(op))
				if op == "|&" {
					return token{}, errUnsupported("|&")
				}
				return token{kind: tOp, text: op}, nil
			}
		}
	}
	w, err := lx.scanWord()
	if err != nil {
		return token{}, err
	}
	return token{kind: tWord, text: w}, nil
}

func (lx *lexer) hasPrefix(s string) bool {
	for i, r := range s {
		if lx.at(lx.pos+i) != r {
			return false
		}
	}
	return true
}

// redirOp consumes a redirection operator at pos (<, >, >>, >|, <<, <<-,
// <<<, <&, >&, <>).
func (lx *lexer) redirOp() string {
	for _, op := range []string{"<<<", "<<-", "<<", ">>", ">|", "<&", ">&", "<>", "<", ">"} {
		if lx.hasPrefix(op) {
			lx.pos += len(op)
			return op
		}
	}
	return ""
}

// scanArith reads a (( ... )) arithmetic block. Parentheses that don't
// close as a pair (a subshell nested in a subshell) report !ok.
func (lx *lexer) scanArith() (string, bool) {
	depth := 0
	for j := lx.pos; j < len(lx.src); j++ {
		switch lx.src[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 1 && lx.at(j+1) == ')' {
				body := string(lx.src[lx.pos+2 : j])
				lx.pos = j + 2
				return body, true
			}
			if depth <= 1 {
				return "", false
			}
		case '\n':
			return "", false
		}
	}
	return "", false
}

// scanWord reads one word, keeping quotes and nested $(...), ${...},
// $((...)) and `...` intact for the expander.
func (lx *lexer) scanWord() (string, error) {
	start := lx.pos
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || isOpStart(c):
			return string(lx.src[start:lx.pos]), nil
		case c == '\\':
			lx.pos += 2
		case c == '\'':
			if err := lx.skipUntil('\'', false); err != nil {
				return "", err
			}
		case c == '"':
			if err := lx.skipDouble(); err != nil {
				return "", err
			}
		case c == '`':
			if err := lx.skipUntil('`', true); err != nil {
				return "", err
			}
		case c == '$':
			if err := lx.skipDollar(); err != nil {
				return "", err
			}
		default:
			lx.pos++
		}
	}
	if lx.pos > len(lx.src) {
		lx.pos = len(lx.src)
	}
	return string(lx.src[start:lx.pos]), nil
}

func (lx *lexer) skipUntil(end rune, escapes bool) error {
	lx.pos++
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if escapes && c == '\\' {
			lx.pos += 2
			continue
		}
		lx.pos++
		if c == end {
			return nil
		}
	}
	return errSyntax(fmt.Sprintf("unterminated %c", end))
}

func (lx *lexer) skipDouble() error {
	lx.pos++
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch c {
		case '\\':
			lx.pos += 2
		case '"':
			lx.pos++
			return nil
		case '`':
			if err := lx.skipUntil('`', true); err != nil {
				return err
			}
		case '$':
			if err := lx.skipDollar(); err != nil {
				return err
			}
		default:
			lx.pos++
		}
	}
	return errSyntax("unterminated \"")
}

func (lx *lexer) skipDollar() error {
	switch lx.at(lx.pos + 1) {
	case '(':
		lx.pos++
		return lx.skipBalanced('(', ')')
	case '{':
		lx.pos++
		return lx.skipBalanced('{', '}')
	case '\'':
		lx.pos++
		return lx.skipUntil('\'', true)
	}
	lx.pos++
	return nil
}

// skipBalanced consumes from an opening bracket to its matching close,
// honoring quotes inside (a $(...) body may itself contain quotes).
func (lx *lexer) skipBalanced(open, close rune) error {
	depth := 0
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch c {
		case '\\':
			lx.pos += 2
			continue
		case '\'':
			if err := lx.skipUntil('\'', false); err != nil {
				return err
			}
			continue
		case '"':
			if err := lx.skipDouble(); err != nil {
				return err
			}
			continue
		case '`':
			if err := lx.skipUntil('`', true); err != nil {
				return err
			}
			continue
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				lx.pos++
				return nil
			}
		}
		lx.pos++
	}
	return errSyntax(fmt.Sprintf("unterminated %c", open))
}

// condWord reads one operand inside [[ ]], where && || < > ( ) ! are
// operators of the conditional, not of the shell.
func (lx *lexer) condWord(regex bool) (string, error) {
	lx.skipBlanks()
	if lx.pos >= len(lx.src) || lx.src[lx.pos] == '\n' {
		return "", errSyntax("unterminated [[")
	}
	if !regex {
		for _, op := range []string{"&&", "||", "(", ")", "<", ">"} {
			if lx.hasPrefix(op) {
				lx.pos += len(op)
				return op, nil
			}
		}
		if lx.hasPrefix("! ") || lx.hasPrefix("!\t") {
			lx.pos++
			return "!", nil
		}
	}
	if regex {
		start, depth := lx.pos, 0
		for lx.pos < len(lx.src) {
			c := lx.src[lx.pos]
			if c == '\\' {
				lx.pos += 2
				continue
			}
			if (c == ' ' || c == '\t' || c == '\n') && depth == 0 {
				break
			}
			if c == '(' || c == '[' {
				depth++
			} else if (c == ')' || c == ']') && depth > 0 {
				depth--
			} else if c == '\'' {
				if err := lx.skipUntil('\'', false); err != nil {
					return "", err
				}
				continue
			} else if c == '"' {
				if err := lx.skipDouble(); err != nil {
					return "", err
				}
				continue
			}
			lx.pos++
		}
		return string(lx.src[start:lx.pos]), nil
	}
	return lx.scanWord()
}

// readHeredocBodies consumes the bodies of heredocs whose operators
// appeared on the line that just ended.
func (lx *lexer) readHeredocBodies() error {
	for _, r := range lx.heredocs {
		var body strings.Builder
		for lx.pos < len(lx.src) {
			end := lx.pos
			for end < len(lx.src) && lx.src[end] != '\n' {
				end++
			}
			line := string(lx.src[lx.pos:end])
			lx.pos = end
			if lx.pos < len(lx.src) {
				lx.pos++
			}
			if r.stripTabs {
				line = strings.TrimLeft(line, "\t")
			}
			if line == r.target {
				break
			}
			body.WriteString(line)
			body.WriteString("\n")
		}
		r.body = body.String()
	}
	lx.heredocs = nil
	return nil
}

type shellError struct {
	msg         string
	unsupported bool
}

func (e *shellError) Error() string { return e.msg }

func errSyntax(msg string) error { return &shellError{msg: "syntax error: " + msg} }

func errUnsupported(what string) error {
	return &shellError{msg: what + " is not supported by the in-browser shell", unsupported: true}
}
