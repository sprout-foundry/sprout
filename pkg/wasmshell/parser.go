package wasmshell

import (
	"regexp"
	"strings"
)

type listNode struct {
	items []*andOrNode
}

type andOrNode struct {
	pipes []*pipelineNode
	ops   []string // ops[i] joins pipes[i] and pipes[i+1]
}

type pipelineNode struct {
	negate bool
	cmds   []command
}

type command interface{}

type redir struct {
	fd        int
	op        string
	target    string
	body      string
	stripTabs bool
	expand    bool
}

type simpleCmd struct {
	assigns []string
	words   []string
	redirs  []*redir
}

type groupCmd struct {
	body     *listNode
	subshell bool
	redirs   []*redir
}

type ifCmd struct {
	conds    []*listNode
	bodies   []*listNode
	elseBody *listNode
	redirs   []*redir
}

type loopCmd struct {
	until  bool
	cond   *listNode
	body   *listNode
	redirs []*redir
}

type forCmd struct {
	varName string
	words   []string
	hasIn   bool
	arith   [3]string
	isArith bool
	body    *listNode
	redirs  []*redir
}

type caseItem struct {
	patterns []string
	body     *listNode
}

type caseCmd struct {
	word   string
	items  []caseItem
	redirs []*redir
}

type funcDef struct {
	name string
	body command
}

type arithCmd struct {
	expr   string
	redirs []*redir
}

type condCmd struct {
	words  []string
	redirs []*redir
}

var (
	assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\+)?=`)
	nameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type parser struct {
	lx *lexer
}

func parseScript(src string) (*listNode, error) {
	p := &parser{lx: newLexer(src)}
	list, err := p.list(nil, false)
	if err != nil {
		return nil, err
	}
	t, err := p.lx.peek()
	if err != nil {
		return nil, err
	}
	if t.kind != tEOF {
		return nil, errSyntax("unexpected " + strings.TrimSpace(t.text))
	}
	return list, nil
}

// advance consumes a token that peek already returned without error.
func (p *parser) advance() {
	_, _ = p.lx.next()
}

func isReserved(t token, words map[string]bool) bool {
	return t.kind == tWord && words[t.text]
}

func (p *parser) skipNewlines() error {
	for {
		t, err := p.lx.peek()
		if err != nil {
			return err
		}
		if t.kind != tNewline {
			return nil
		}
		p.advance()
	}
}

// list parses and-or lists separated by ; & or newlines, stopping at EOF,
// at a reserved word in stop, at ")" when closeParen, or at ";;".
func (p *parser) list(stop map[string]bool, closeParen bool) (*listNode, error) {
	l := &listNode{}
	for {
		if err := p.skipNewlines(); err != nil {
			return nil, err
		}
		t, err := p.lx.peek()
		if err != nil {
			return nil, err
		}
		if t.kind == tEOF || isReserved(t, stop) || (t.kind == tOp && (t.text == ";;" || (closeParen && t.text == ")"))) {
			return l, nil
		}
		ao, err := p.andOr()
		if err != nil {
			return nil, err
		}
		l.items = append(l.items, ao)
		t, err = p.lx.peek()
		if err != nil {
			return nil, err
		}
		if t.kind == tOp && (t.text == ";" || t.text == "&") {
			p.advance()
		}
	}
}

func (p *parser) andOr() (*andOrNode, error) {
	ao := &andOrNode{}
	for {
		pl, err := p.pipeline()
		if err != nil {
			return nil, err
		}
		ao.pipes = append(ao.pipes, pl)
		t, err := p.lx.peek()
		if err != nil {
			return nil, err
		}
		if t.kind != tOp || (t.text != "&&" && t.text != "||") {
			return ao, nil
		}
		p.advance()
		ao.ops = append(ao.ops, t.text)
		if err := p.skipNewlines(); err != nil {
			return nil, err
		}
	}
}

func (p *parser) pipeline() (*pipelineNode, error) {
	pl := &pipelineNode{}
	t, err := p.lx.peek()
	if err != nil {
		return nil, err
	}
	if t.kind == tWord && t.text == "!" {
		p.advance()
		pl.negate = true
	}
	for {
		c, err := p.command()
		if err != nil {
			return nil, err
		}
		pl.cmds = append(pl.cmds, c)
		t, err := p.lx.peek()
		if err != nil {
			return nil, err
		}
		if t.kind != tOp || t.text != "|" {
			return pl, nil
		}
		p.advance()
		if err := p.skipNewlines(); err != nil {
			return nil, err
		}
	}
}

func words(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

func (p *parser) expectWord(w string) error {
	if err := p.skipNewlines(); err != nil {
		return err
	}
	t, err := p.lx.next()
	if err != nil {
		return err
	}
	if t.kind != tWord || t.text != w {
		got := t.text
		if t.kind == tEOF {
			got = "end of input"
		}
		return errSyntax("expected '" + w + "' but found '" + strings.TrimSpace(got) + "'")
	}
	return nil
}

func (p *parser) command() (command, error) {
	t, err := p.lx.peek()
	if err != nil {
		return nil, err
	}
	switch {
	case t.kind == tArith:
		p.advance()
		c := &arithCmd{expr: t.text}
		return c, p.redirects(&c.redirs)
	case t.kind == tOp && t.text == "(":
		p.advance()
		body, err := p.list(nil, true)
		if err != nil {
			return nil, err
		}
		if t, err := p.lx.next(); err != nil || t.kind != tOp || t.text != ")" {
			return nil, errSyntax("expected ')'")
		}
		c := &groupCmd{body: body, subshell: true}
		return c, p.redirects(&c.redirs)
	case t.kind == tWord:
		switch t.text {
		case "{":
			p.advance()
			body, err := p.list(words("}"), false)
			if err != nil {
				return nil, err
			}
			if err := p.expectWord("}"); err != nil {
				return nil, err
			}
			c := &groupCmd{body: body}
			return c, p.redirects(&c.redirs)
		case "if":
			return p.ifClause()
		case "while", "until":
			return p.loop(t.text == "until")
		case "for":
			return p.forClause()
		case "case":
			return p.caseClause()
		case "function":
			p.advance()
			name, err := p.lx.next()
			if err != nil {
				return nil, err
			}
			if nt, _ := p.lx.peek(); nt.kind == tOp && nt.text == "(" {
				p.advance()
				if ct, err := p.lx.next(); err != nil || ct.text != ")" {
					return nil, errSyntax("expected ')'")
				}
			}
			return p.funcBody(name.text)
		case "[[":
			return p.condClause()
		case "select", "coproc":
			return nil, errUnsupported(t.text)
		}
	}
	return p.simple()
}

func (p *parser) funcBody(name string) (command, error) {
	if err := p.skipNewlines(); err != nil {
		return nil, err
	}
	body, err := p.command()
	if err != nil {
		return nil, err
	}
	return &funcDef{name: name, body: body}, nil
}

func (p *parser) redirects(rs *[]*redir) error {
	for {
		t, err := p.lx.peek()
		if err != nil {
			return err
		}
		if t.kind != tRedir {
			return nil
		}
		p.advance()
		r, err := p.redirect(t)
		if err != nil {
			return err
		}
		*rs = append(*rs, r)
	}
}

func (p *parser) redirect(t token) (*redir, error) {
	target, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	if target.kind != tWord {
		return nil, errSyntax("missing redirection target after " + t.text)
	}
	r := &redir{fd: t.fd, op: t.text, target: target.text}
	if t.text == "<<" || t.text == "<<-" {
		r.stripTabs = t.text == "<<-"
		r.expand = !strings.ContainsAny(target.text, `'"\`)
		r.target = unquoteLiteral(target.text)
		p.lx.heredocs = append(p.lx.heredocs, r)
	}
	return r, nil
}

func (p *parser) simple() (command, error) {
	c := &simpleCmd{}
	for {
		t, err := p.lx.peek()
		if err != nil {
			return nil, err
		}
		switch t.kind {
		case tWord:
			p.advance()
			if len(c.words) == 0 && assignRe.MatchString(t.text) {
				if nt, _ := p.lx.peek(); nt.kind == tOp && nt.text == "(" && strings.HasSuffix(t.text, "=") {
					return nil, errUnsupported("array assignment")
				}
				c.assigns = append(c.assigns, t.text)
				continue
			}
			c.words = append(c.words, t.text)
			if len(c.words) == 1 && len(c.assigns) == 0 && nameRe.MatchString(t.text) {
				if nt, _ := p.lx.peek(); nt.kind == tOp && nt.text == "(" {
					p.advance()
					if ct, err := p.lx.next(); err != nil || ct.kind != tOp || ct.text != ")" {
						return nil, errSyntax("expected ')' in function definition")
					}
					return p.funcBody(t.text)
				}
			}
		case tRedir:
			p.advance()
			r, err := p.redirect(t)
			if err != nil {
				return nil, err
			}
			c.redirs = append(c.redirs, r)
		default:
			if len(c.words) == 0 && len(c.assigns) == 0 && len(c.redirs) == 0 {
				if t.kind == tEOF {
					return nil, errSyntax("unexpected end of input")
				}
				return nil, errSyntax("unexpected '" + strings.TrimSpace(t.text) + "'")
			}
			return c, nil
		}
	}
}
