package wasmshell

import (
	"strconv"
)

type jqNode interface{}

type (
	jqIdentity struct{}
	jqRecurse  struct{}
	jqLiteral  struct{ v any }
	jqField    struct {
		target jqNode
		name   string
		opt    bool
	}
	jqIndex struct {
		target jqNode
		index  jqNode
		opt    bool
	}
	jqSlice struct {
		target   jqNode
		from, to jqNode
	}
	jqIterate struct {
		target jqNode
		opt    bool
	}
	jqPipe   struct{ l, r jqNode }
	jqComma  struct{ l, r jqNode }
	jqBinary struct {
		op   string
		l, r jqNode
	}
	jqNeg    struct{ x jqNode }
	jqArray  struct{ body jqNode }
	jqObject struct{ entries []jqEntry }
	jqVarRef struct{ name string }
	jqCall   struct {
		name string
		args []jqNode
	}
	jqIf struct {
		conds, thens []jqNode
		els          jqNode
	}
	jqStringNode struct{ parts []jqStrPart }
	jqFormat     struct{ name string }
	jqTry        struct{ x jqNode }
	jqAs         struct {
		src  jqNode
		name string
		body jqNode
	}
	jqReduce struct {
		src       jqNode
		name      string
		init, upd jqNode
	}
)

type jqEntry struct {
	keyExpr jqNode
	keyName string
	value   jqNode
}

type jqParser struct {
	toks []jqTok
	pos  int
}

func parseJq(src string) (node jqNode, err error) {
	toks, err := jqLex(src)
	if err != nil {
		return nil, err
	}
	p := &jqParser{toks: toks}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(jqError); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	if p.peek().kind == "eof" {
		return jqIdentity{}, nil
	}
	node = p.pipe()
	if p.peek().kind != "eof" {
		p.fail("unexpected token")
	}
	return node, nil
}

type jqError struct{ msg string }

func (e jqError) Error() string { return e.msg }

func (p *jqParser) fail(msg string) {
	t := p.peek()
	near := t.text
	if t.kind == "eof" {
		near = "end of filter"
	}
	panic(jqError{"syntax error: " + msg + " near '" + near + "'"})
}

func (p *jqParser) peek() jqTok { return p.toks[p.pos] }

func (p *jqParser) isOp(op string) bool {
	t := p.peek()
	return t.kind == "op" && t.text == op
}

func (p *jqParser) isIdent(w string) bool {
	t := p.peek()
	return t.kind == "ident" && t.text == w
}

func (p *jqParser) expect(op string) {
	if !p.isOp(op) {
		p.fail("expected " + op)
	}
	p.pos++
}

func (p *jqParser) expectIdent(w string) {
	if !p.isIdent(w) {
		p.fail("expected " + w)
	}
	p.pos++
}

func (p *jqParser) pipe() jqNode {
	if p.isIdent("def") {
		p.fail("def is not supported by the in-browser shell")
	}
	l := p.comma()
	if p.isIdent("as") {
		p.pos++
		v := p.peek()
		if v.kind != "var" {
			p.fail("expected $name after as")
		}
		p.pos++
		p.expect("|")
		return &jqAs{src: l, name: v.text, body: p.pipe()}
	}
	if p.isOp("|") {
		p.pos++
		return &jqPipe{l: l, r: p.pipe()}
	}
	return l
}

func (p *jqParser) comma() jqNode {
	l := p.alt()
	for p.isOp(",") {
		p.pos++
		l = &jqComma{l: l, r: p.alt()}
	}
	return l
}

func (p *jqParser) alt() jqNode {
	l := p.or()
	for p.isOp("//") {
		p.pos++
		l = &jqBinary{op: "//", l: l, r: p.or()}
	}
	for _, op := range []string{"|=", "+=", "-=", "*=", "/=", "//=", "="} {
		if p.isOp(op) {
			p.fail("assignment is not supported by the in-browser shell")
		}
	}
	return l
}

func (p *jqParser) or() jqNode {
	l := p.and()
	for p.isIdent("or") {
		p.pos++
		l = &jqBinary{op: "or", l: l, r: p.and()}
	}
	return l
}

func (p *jqParser) and() jqNode {
	l := p.cmp()
	for p.isIdent("and") {
		p.pos++
		l = &jqBinary{op: "and", l: l, r: p.cmp()}
	}
	return l
}

func (p *jqParser) cmp() jqNode {
	l := p.add()
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if p.isOp(op) {
			p.pos++
			return &jqBinary{op: op, l: l, r: p.add()}
		}
	}
	return l
}

func (p *jqParser) add() jqNode {
	l := p.mul()
	for p.isOp("+") || p.isOp("-") {
		op := p.peek().text
		p.pos++
		l = &jqBinary{op: op, l: l, r: p.mul()}
	}
	return l
}

func (p *jqParser) mul() jqNode {
	l := p.postfix()
	for p.isOp("*") || p.isOp("/") || p.isOp("%") {
		op := p.peek().text
		p.pos++
		l = &jqBinary{op: op, l: l, r: p.postfix()}
	}
	return l
}

func (p *jqParser) postfix() jqNode {
	x := p.primary()
	for {
		switch {
		case p.peek().kind == "field":
			x = &jqField{target: x, name: p.peek().text}
			p.pos++
		case p.isOp(".") && p.toks[p.pos+1].kind == "str":
			p.pos++
			x = &jqField{target: x, name: p.peek().parts[0].lit}
			p.pos++
		case p.isOp(".") && p.toks[p.pos+1].kind == "op" && p.toks[p.pos+1].text == "[":
			p.pos++
		case p.isOp("["):
			x = p.bracket(x)
		case p.isOp("?"):
			p.pos++
			switch t := x.(type) {
			case *jqField:
				t.opt = true
			case *jqIndex:
				t.opt = true
			case *jqIterate:
				t.opt = true
			default:
				x = &jqTry{x: x}
			}
		default:
			return x
		}
	}
}

func (p *jqParser) bracket(target jqNode) jqNode {
	p.expect("[")
	if p.isOp("]") {
		p.pos++
		return &jqIterate{target: target}
	}
	if p.isOp(":") {
		p.pos++
		to := p.pipe()
		p.expect("]")
		return &jqSlice{target: target, to: to}
	}
	idx := p.pipe()
	if p.isOp(":") {
		p.pos++
		var to jqNode
		if !p.isOp("]") {
			to = p.pipe()
		}
		p.expect("]")
		return &jqSlice{target: target, from: idx, to: to}
	}
	p.expect("]")
	return &jqIndex{target: target, index: idx}
}

func (p *jqParser) primary() jqNode {
	t := p.peek()
	switch t.kind {
	case "num":
		p.pos++
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			p.fail("bad number")
		}
		return &jqLiteral{v: f}
	case "str":
		p.pos++
		if len(t.parts) == 1 {
			return &jqLiteral{v: t.parts[0].lit}
		}
		return &jqStringNode{parts: t.parts}
	case "field":
		p.pos++
		return &jqField{target: jqIdentity{}, name: t.text}
	case "var":
		p.pos++
		return &jqVarRef{name: t.text}
	case "format":
		p.pos++
		if p.peek().kind == "str" {
			s := p.peek()
			p.pos++
			return &jqPipe{l: &jqStringNode{parts: s.parts}, r: &jqFormat{name: t.text}}
		}
		return &jqFormat{name: t.text}
	case "ident":
		switch t.text {
		case "true", "false":
			p.pos++
			return &jqLiteral{v: t.text == "true"}
		case "null":
			p.pos++
			return &jqLiteral{v: nil}
		case "if":
			return p.ifExpr()
		case "try":
			p.pos++
			x := p.postfix()
			if p.isIdent("catch") {
				p.pos++
				p.postfix()
			}
			return &jqTry{x: x}
		case "reduce":
			p.pos++
			src := p.postfix()
			p.expectIdent("as")
			v := p.peek()
			if v.kind != "var" {
				p.fail("expected $name")
			}
			p.pos++
			p.expect("(")
			init := p.pipe()
			p.expect(";")
			upd := p.pipe()
			p.expect(")")
			return &jqReduce{src: src, name: v.text, init: init, upd: upd}
		case "foreach", "label", "def", "import", "include":
			p.fail(t.text + " is not supported by the in-browser shell")
		}
		p.pos++
		c := &jqCall{name: t.text}
		if p.isOp("(") {
			p.pos++
			for {
				c.args = append(c.args, p.pipe())
				if p.isOp(";") {
					p.pos++
					continue
				}
				break
			}
			p.expect(")")
		}
		return c
	case "op":
		switch t.text {
		case ".":
			p.pos++
			if p.peek().kind == "str" {
				s := p.peek()
				p.pos++
				return &jqField{target: jqIdentity{}, name: s.parts[0].lit}
			}
			if p.isOp("[") {
				return p.bracket(jqIdentity{})
			}
			return jqIdentity{}
		case "..":
			p.pos++
			return jqRecurse{}
		case "(":
			p.pos++
			x := p.pipe()
			p.expect(")")
			return x
		case "[":
			p.pos++
			if p.isOp("]") {
				p.pos++
				return &jqArray{}
			}
			body := p.pipe()
			p.expect("]")
			return &jqArray{body: body}
		case "{":
			return p.object()
		case "-":
			p.pos++
			return &jqNeg{x: p.postfix()}
		}
	}
	p.fail("unexpected token")
	return nil
}

func (p *jqParser) ifExpr() jqNode {
	p.pos++
	n := &jqIf{}
	for {
		n.conds = append(n.conds, p.pipe())
		p.expectIdent("then")
		n.thens = append(n.thens, p.pipe())
		if p.isIdent("elif") {
			p.pos++
			continue
		}
		break
	}
	if p.isIdent("else") {
		p.pos++
		n.els = p.pipe()
	}
	p.expectIdent("end")
	return n
}

func (p *jqParser) object() jqNode {
	p.expect("{")
	o := &jqObject{}
	for !p.isOp("}") {
		var e jqEntry
		t := p.peek()
		switch {
		case t.kind == "ident":
			e.keyName = t.text
			p.pos++
		case t.kind == "var":
			e.keyName = t.text
			e.value = &jqVarRef{name: t.text}
			p.pos++
		case t.kind == "str":
			p.pos++
			if len(t.parts) == 1 {
				e.keyName = t.parts[0].lit
			} else {
				e.keyExpr = &jqStringNode{parts: t.parts}
			}
		case t.kind == "op" && t.text == "(":
			p.pos++
			e.keyExpr = p.pipe()
			p.expect(")")
		default:
			p.fail("bad object key")
		}
		if p.isOp(":") {
			p.pos++
			e.value = p.objectValue()
		} else if e.value == nil {
			if e.keyExpr != nil {
				p.fail("expected : after computed key")
			}
			e.value = &jqField{target: jqIdentity{}, name: e.keyName}
		}
		o.entries = append(o.entries, e)
		if p.isOp(",") {
			p.pos++
			continue
		}
		if !p.isOp("}") {
			p.fail("expected , or }")
		}
	}
	p.pos++
	return o
}

// objectValue parses an object value: a pipe without top-level commas.
func (p *jqParser) objectValue() jqNode {
	l := p.alt()
	for p.isOp("|") {
		p.pos++
		l = &jqPipe{l: l, r: p.alt()}
	}
	return l
}
