package wasmshell

import (
	"strings"
)

func (p *parser) ifClause() (command, error) {
	p.advance()
	c := &ifCmd{}
	for {
		cond, err := p.list(words("then"), false)
		if err != nil {
			return nil, err
		}
		if err := p.expectWord("then"); err != nil {
			return nil, err
		}
		body, err := p.list(words("elif", "else", "fi"), false)
		if err != nil {
			return nil, err
		}
		c.conds = append(c.conds, cond)
		c.bodies = append(c.bodies, body)
		t, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		switch t.text {
		case "elif":
			continue
		case "else":
			if c.elseBody, err = p.list(words("fi"), false); err != nil {
				return nil, err
			}
			if err := p.expectWord("fi"); err != nil {
				return nil, err
			}
		case "fi":
		default:
			return nil, errSyntax("expected 'fi'")
		}
		return c, p.redirects(&c.redirs)
	}
}

func (p *parser) doBody() (*listNode, error) {
	if err := p.expectWord("do"); err != nil {
		return nil, err
	}
	body, err := p.list(words("done"), false)
	if err != nil {
		return nil, err
	}
	return body, p.expectWord("done")
}

func (p *parser) loop(until bool) (command, error) {
	p.advance()
	cond, err := p.list(words("do"), false)
	if err != nil {
		return nil, err
	}
	body, err := p.doBody()
	if err != nil {
		return nil, err
	}
	c := &loopCmd{until: until, cond: cond, body: body}
	return c, p.redirects(&c.redirs)
}

func (p *parser) forClause() (command, error) {
	p.advance()
	c := &forCmd{}
	t, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	if t.kind == tArith {
		parts := strings.Split(t.text, ";")
		if len(parts) != 3 {
			return nil, errSyntax("expected for ((init; cond; step))")
		}
		c.isArith = true
		copy(c.arith[:], parts)
	} else {
		if t.kind != tWord || !nameRe.MatchString(t.text) {
			return nil, errSyntax("bad for loop variable")
		}
		c.varName = t.text
		if err := p.skipNewlines(); err != nil {
			return nil, err
		}
		if nt, _ := p.lx.peek(); nt.kind == tWord && nt.text == "in" {
			p.advance()
			c.hasIn = true
			for {
				wt, err := p.lx.peek()
				if err != nil {
					return nil, err
				}
				if wt.kind != tWord {
					break
				}
				p.advance()
				c.words = append(c.words, wt.text)
			}
		}
	}
	if nt, _ := p.lx.peek(); nt.kind == tOp && nt.text == ";" {
		p.advance()
	}
	if c.body, err = p.doBody(); err != nil {
		return nil, err
	}
	return c, p.redirects(&c.redirs)
}

func (p *parser) caseClause() (command, error) {
	p.advance()
	w, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	c := &caseCmd{word: w.text}
	if err := p.expectWord("in"); err != nil {
		return nil, err
	}
	for {
		if err := p.skipNewlines(); err != nil {
			return nil, err
		}
		t, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		if t.kind == tWord && t.text == "esac" {
			break
		}
		if t.kind == tOp && t.text == "(" {
			if t, err = p.lx.next(); err != nil {
				return nil, err
			}
		}
		var item caseItem
		for {
			if t.kind != tWord {
				return nil, errSyntax("bad case pattern")
			}
			item.patterns = append(item.patterns, t.text)
			sep, err := p.lx.next()
			if err != nil {
				return nil, err
			}
			if sep.kind == tOp && sep.text == ")" {
				break
			}
			if sep.kind != tOp || sep.text != "|" {
				return nil, errSyntax("expected ')' after case pattern")
			}
			if t, err = p.lx.next(); err != nil {
				return nil, err
			}
		}
		if item.body, err = p.list(words("esac"), false); err != nil {
			return nil, err
		}
		c.items = append(c.items, item)
		if nt, _ := p.lx.peek(); nt.kind == tOp && nt.text == ";;" {
			p.advance()
		}
	}
	return c, p.redirects(&c.redirs)
}

func (p *parser) condClause() (command, error) {
	p.advance()
	if p.lx.peeked != nil {
		return nil, errSyntax("unexpected token after [[")
	}
	c := &condCmd{}
	regexNext := false
	for {
		w, err := p.lx.condWord(regexNext)
		if err != nil {
			return nil, err
		}
		if w == "]]" && !regexNext {
			break
		}
		regexNext = w == "=~"
		c.words = append(c.words, w)
	}
	return c, p.redirects(&c.redirs)
}

// unquoteLiteral removes quoting from a word without expanding anything
// (heredoc delimiters).
func unquoteLiteral(w string) string {
	var b strings.Builder
	inS, inD := false, false
	for i := 0; i < len(w); i++ {
		c := w[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '\\' && !inS && i+1 < len(w):
			i++
			b.WriteByte(w[i])
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
