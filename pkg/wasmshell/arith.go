package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

// arith evaluates shell arithmetic ($((...)), ((...)), let) over int64,
// with C operator precedence and assignment to shell variables.
func (sh *interp) arith(expr string) (int64, error) {
	expanded, err := sh.expandString(expr)
	if err != nil {
		return 0, err
	}
	a := &arithParser{sh: sh, src: expanded}
	a.lex()
	if a.err != nil {
		return 0, a.err
	}
	if len(a.toks) == 0 {
		return 0, nil
	}
	v := a.comma()
	if a.err == nil && a.pos < len(a.toks) {
		a.err = fmt.Errorf("arithmetic syntax error near %q", a.toks[a.pos])
	}
	return v, a.err
}

type arithParser struct {
	sh   *interp
	src  string
	toks []string
	pos  int
	err  error
}

var arithOps = []string{
	"<<=", ">>=", "**", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
	"+", "-", "*", "/", "%", "<", ">", "=", "!", "~", "&", "|", "^", "?", ":", "(", ")", ",",
}

func (a *arithParser) lex() {
	s := a.src
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case isDigit(c):
			j := i
			for j < len(s) && (isAlnum(s[j]) || (s[j] == '#' && j+1 < len(s) && isAlnum(s[j+1]))) {
				j++
			}
			a.toks = append(a.toks, s[i:j])
			i = j
		case c == '_' || isAlpha(c):
			j := i
			for j < len(s) && (s[j] == '_' || isAlnum(s[j])) {
				j++
			}
			a.toks = append(a.toks, s[i:j])
			i = j
		default:
			matched := false
			for _, op := range arithOps {
				if strings.HasPrefix(s[i:], op) {
					a.toks = append(a.toks, op)
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				a.err = fmt.Errorf("arithmetic syntax error: unexpected %q", string(c))
				return
			}
		}
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }

func (a *arithParser) peek() string {
	if a.pos < len(a.toks) {
		return a.toks[a.pos]
	}
	return ""
}

func (a *arithParser) take(op string) bool {
	if a.peek() == op {
		a.pos++
		return true
	}
	return false
}

func (a *arithParser) comma() int64 {
	v := a.assign()
	for a.take(",") {
		v = a.assign()
	}
	return v
}

func isIdent(t string) bool {
	return t != "" && (t[0] == '_' || isAlpha(t[0]))
}

func (a *arithParser) assign() int64 {
	if a.pos+1 < len(a.toks) && isIdent(a.toks[a.pos]) {
		name, op := a.toks[a.pos], a.toks[a.pos+1]
		if op == "=" || (strings.HasSuffix(op, "=") && len(op) >= 2 && op != "==" && op != "!=" && op != "<=" && op != ">=") {
			a.pos += 2
			rhs := a.assign()
			v := rhs
			if op != "=" {
				v = a.binary(strings.TrimSuffix(op, "="), a.variable(name), rhs)
			}
			a.setVar(name, v)
			return v
		}
	}
	return a.ternary()
}

func (a *arithParser) ternary() int64 {
	c := a.logic(0)
	if a.take("?") {
		x := a.assign()
		if !a.take(":") {
			a.fail("expected ':'")
			return 0
		}
		y := a.assign()
		if c != 0 {
			return x
		}
		return y
	}
	return c
}

var arithLevels = [][]string{
	{"||"}, {"&&"}, {"|"}, {"^"}, {"&"}, {"==", "!="}, {"<", ">", "<=", ">="}, {"<<", ">>"}, {"+", "-"}, {"*", "/", "%"},
}

func (a *arithParser) logic(level int) int64 {
	if level == len(arithLevels) {
		return a.power()
	}
	v := a.logic(level + 1)
	for {
		op := a.peek()
		found := false
		for _, o := range arithLevels[level] {
			if op == o {
				found = true
			}
		}
		if !found {
			return v
		}
		a.pos++
		v = a.binary(op, v, a.logic(level+1))
	}
}

func (a *arithParser) power() int64 {
	base := a.unary()
	if a.take("**") {
		exp := a.power()
		r := int64(1)
		for i := int64(0); i < exp; i++ {
			r *= base
		}
		return r
	}
	return base
}

func (a *arithParser) unary() int64 {
	switch a.peek() {
	case "-":
		a.pos++
		return -a.unary()
	case "+":
		a.pos++
		return a.unary()
	case "!":
		a.pos++
		return boolInt(a.unary() == 0)
	case "~":
		a.pos++
		return ^a.unary()
	case "++", "--":
		op := a.toks[a.pos]
		a.pos++
		name := a.peek()
		if !isIdent(name) {
			a.fail("expected variable after " + op)
			return 0
		}
		a.pos++
		v := a.variable(name) + map[string]int64{"++": 1, "--": -1}[op]
		a.setVar(name, v)
		return v
	}
	return a.postfix()
}

func (a *arithParser) postfix() int64 {
	t := a.peek()
	if t == "" {
		a.fail("expression expected")
		return 0
	}
	a.pos++
	if t == "(" {
		v := a.comma()
		if !a.take(")") {
			a.fail("expected ')'")
		}
		return v
	}
	if isIdent(t) {
		v := a.variable(t)
		if op := a.peek(); op == "++" || op == "--" {
			a.pos++
			a.setVar(t, v+map[string]int64{"++": 1, "--": -1}[op])
		}
		return v
	}
	n, err := parseArithNumber(t)
	if err != nil {
		a.fail(err.Error())
	}
	return n
}

func parseArithNumber(t string) (int64, error) {
	switch {
	case strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X"):
		return strconv.ParseInt(t[2:], 16, 64)
	case strings.Contains(t, "#"):
		base, digits, _ := strings.Cut(t, "#")
		b, err := strconv.Atoi(base)
		if err != nil {
			return 0, err
		}
		return strconv.ParseInt(digits, b, 64)
	case len(t) > 1 && t[0] == '0':
		return strconv.ParseInt(t[1:], 8, 64)
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: value too great for base or not a number", t)
	}
	return n, nil
}

func (a *arithParser) variable(name string) int64 {
	v, _ := a.sh.lookup(name)
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := parseArithNumber(v); err == nil {
		return n
	}
	if isIdent(v) && v != name {
		return a.variable(v)
	}
	a.fail(v + ": not a number")
	return 0
}

func (a *arithParser) setVar(name string, v int64) {
	a.sh.setVar(name, strconv.FormatInt(v, 10))
}

func (a *arithParser) fail(msg string) {
	if a.err == nil {
		a.err = fmt.Errorf("arithmetic: %s", msg)
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (a *arithParser) binary(op string, x, y int64) int64 {
	switch op {
	case "+":
		return x + y
	case "-":
		return x - y
	case "*":
		return x * y
	case "/", "%":
		if y == 0 {
			a.fail("division by 0")
			return 0
		}
		if op == "/" {
			return x / y
		}
		return x % y
	case "<<", ">>":
		if y < 0 || y > 63 {
			a.fail("shift count out of range")
			return 0
		}
		if op == "<<" {
			return x << y
		}
		return x >> y
	case "&":
		return x & y
	case "|":
		return x | y
	case "^":
		return x ^ y
	case "&&":
		return boolInt(x != 0 && y != 0)
	case "||":
		return boolInt(x != 0 || y != 0)
	case "==":
		return boolInt(x == y)
	case "!=":
		return boolInt(x != y)
	case "<":
		return boolInt(x < y)
	case ">":
		return boolInt(x > y)
	case "<=":
		return boolInt(x <= y)
	case ">=":
		return boolInt(x >= y)
	}
	a.fail("unknown operator " + op)
	return 0
}
