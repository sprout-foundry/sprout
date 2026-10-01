package wasmshell

import (
	"regexp"
)

type (
	aNum   struct{ v float64 }
	aStr   struct{ v string }
	aRegex struct{ re *regexp.Regexp }
	aVar   struct{ name string }
	aField struct{ idx awkExpr }
	aIndex struct {
		name string
		keys []awkExpr
	}
	aAssign struct {
		op       string
		lhs, rhs awkExpr
	}
	aCond   struct{ c, a, b awkExpr }
	aBinary struct {
		op   string
		l, r awkExpr
	}
	aUnary struct {
		op string
		x  awkExpr
	}
	aIncDec struct {
		op  string
		pre bool
		lv  awkExpr
	}
	aCall struct {
		name string
		args []awkExpr
	}
	aIn struct {
		keys []awkExpr
		arr  string
	}
	aGroup struct{ list []awkExpr }
)

type awkExpr interface{}

type awkStmt interface{}

type (
	aPrint struct {
		printf bool
		args   []awkExpr
		dest   awkExpr
		append bool
	}
	aExprStmt struct{ x awkExpr }
	aIf       struct {
		c         awkExpr
		then, els awkStmt
	}
	aWhile struct {
		c    awkExpr
		body awkStmt
		do   bool
	}
	aFor struct {
		init, post awkStmt
		c          awkExpr
		body       awkStmt
	}
	aForIn struct {
		v, arr string
		body   awkStmt
	}
	aBlock  []awkStmt
	aNext   struct{}
	aExit   struct{ code awkExpr }
	aReturn struct{ x awkExpr }
	aBreak  struct{}
	aCont   struct{}
	aDelete struct {
		name string
		keys []awkExpr
	}
)

type awkItem struct {
	kind    string // "BEGIN", "END", "main"
	pattern awkExpr
	end     awkExpr // range patterns
	body    aBlock
	inRange bool
}

type awkFunc struct {
	params []string
	body   aBlock
}

type awkProg struct {
	items []*awkItem
	funcs map[string]*awkFunc
}

type awkParser struct {
	toks  []awkTok
	pos   int
	noGT  int
	noIn  int
	funcs map[string]*awkFunc
}

func parseAwk(src string) (prog *awkProg, err error) {
	toks, err := awkLex(src)
	if err != nil {
		return nil, err
	}
	p := &awkParser{toks: toks, funcs: map[string]*awkFunc{}}
	prog = &awkProg{funcs: p.funcs}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(awkSyntaxError); ok {
				prog, err = nil, e
				return
			}
			panic(r)
		}
	}()
	for {
		p.skipTerms()
		t := p.peek()
		if t.kind == "eof" {
			break
		}
		if t.kind == "kw" && (t.text == "function" || t.text == "func") {
			p.function()
			continue
		}
		item := &awkItem{kind: "main"}
		switch {
		case t.kind == "kw" && (t.text == "BEGIN" || t.text == "END"):
			p.pos++
			item.kind = t.text
		case !p.isOp("{"):
			item.pattern = p.expr()
			if p.isOp(",") {
				p.pos++
				p.skipNL()
				item.end = p.expr()
			}
		}
		if p.isOp("{") {
			item.body = p.block()
		} else if item.kind != "main" {
			p.fail("expected { after " + item.kind)
		}
		prog.items = append(prog.items, item)
	}
	return prog, err
}

type awkSyntaxError struct{ msg string }

func (e awkSyntaxError) Error() string { return "syntax error: " + e.msg }

func (p *awkParser) fail(msg string) {
	t := p.peek()
	near := t.text
	switch t.kind {
	case "nl":
		near = "newline"
	case "eof":
		near = "end of program"
	}
	panic(awkSyntaxError{msg + " near " + near})
}

func (p *awkParser) peek() awkTok { return p.toks[p.pos] }

func (p *awkParser) isOp(op string) bool {
	t := p.peek()
	return t.kind == "op" && t.text == op
}

func (p *awkParser) isKw(kw string) bool {
	t := p.peek()
	return t.kind == "kw" && t.text == kw
}

func (p *awkParser) expectOp(op string) {
	if !p.isOp(op) {
		p.fail("expected " + op)
	}
	p.pos++
}

func (p *awkParser) skipNL() {
	for p.peek().kind == "nl" {
		p.pos++
	}
}

func (p *awkParser) skipTerms() {
	for p.peek().kind == "nl" || p.isOp(";") {
		p.pos++
	}
}

func (p *awkParser) function() {
	p.pos++
	name := p.peek()
	if name.kind != "name" {
		p.fail("expected function name")
	}
	p.pos++
	p.expectOp("(")
	f := &awkFunc{}
	for !p.isOp(")") {
		t := p.peek()
		if t.kind != "name" {
			p.fail("expected parameter name")
		}
		f.params = append(f.params, t.text)
		p.pos++
		if p.isOp(",") {
			p.pos++
			p.skipNL()
		}
	}
	p.pos++
	p.skipNL()
	p.funcs[name.text] = f
	f.body = p.block()
}

func (p *awkParser) block() aBlock {
	p.expectOp("{")
	var b aBlock
	for {
		p.skipTerms()
		if p.isOp("}") {
			p.pos++
			return b
		}
		if p.peek().kind == "eof" {
			p.fail("unexpected end of program")
		}
		b = append(b, p.stmt())
	}
}

func (p *awkParser) endStmt() {
	if p.isOp(";") || p.peek().kind == "nl" {
		p.pos++
		return
	}
	if p.isOp("}") || p.peek().kind == "eof" {
		return
	}
	p.fail("expected end of statement")
}

func (p *awkParser) simpleOrBlock() awkStmt {
	p.skipNL()
	if p.isOp(";") {
		p.pos++
		return aBlock{}
	}
	return p.stmt()
}

func (p *awkParser) stmt() awkStmt {
	if p.isOp("{") {
		return p.block()
	}
	t := p.peek()
	if t.kind == "kw" {
		switch t.text {
		case "if":
			p.pos++
			p.expectOp("(")
			c := p.expr()
			p.expectOp(")")
			s := &aIf{c: c, then: p.simpleOrBlock()}
			save := p.pos
			p.skipTerms()
			if p.isKw("else") {
				p.pos++
				s.els = p.simpleOrBlock()
			} else {
				p.pos = save
			}
			return s
		case "while":
			p.pos++
			p.expectOp("(")
			c := p.expr()
			p.expectOp(")")
			if p.isOp(";") {
				p.pos++
				return &aWhile{c: c, body: aBlock{}}
			}
			return &aWhile{c: c, body: p.simpleOrBlock()}
		case "do":
			p.pos++
			body := p.simpleOrBlock()
			p.skipTerms()
			if !p.isKw("while") {
				p.fail("expected while after do")
			}
			p.pos++
			p.expectOp("(")
			c := p.expr()
			p.expectOp(")")
			p.endStmt()
			return &aWhile{c: c, body: body, do: true}
		case "for":
			return p.forStmt()
		case "next", "nextfile":
			p.pos++
			p.endStmt()
			return aNext{}
		case "break":
			p.pos++
			p.endStmt()
			return aBreak{}
		case "continue":
			p.pos++
			p.endStmt()
			return aCont{}
		case "exit", "return":
			p.pos++
			var x awkExpr
			if !p.isOp(";") && !p.isOp("}") && p.peek().kind != "nl" && p.peek().kind != "eof" {
				x = p.expr()
			}
			p.endStmt()
			if t.text == "exit" {
				return &aExit{code: x}
			}
			return &aReturn{x: x}
		case "delete":
			p.pos++
			name := p.peek()
			if name.kind != "name" {
				p.fail("expected array name")
			}
			p.pos++
			d := &aDelete{name: name.text}
			if p.isOp("[") {
				p.pos++
				d.keys = p.exprList("]")
			}
			p.endStmt()
			return d
		case "print", "printf":
			return p.printStmt()
		case "getline":
			p.fail("getline is not supported by the in-browser shell")
		}
	}
	x := p.expr()
	p.endStmt()
	return &aExprStmt{x: x}
}

func (p *awkParser) forStmt() awkStmt {
	p.pos++
	p.expectOp("(")
	if p.peek().kind == "name" && p.toks[p.pos+1].kind == "kw" && p.toks[p.pos+1].text == "in" &&
		p.toks[p.pos+2].kind == "name" && p.toks[p.pos+3].kind == "op" && p.toks[p.pos+3].text == ")" {
		v, arr := p.peek().text, p.toks[p.pos+2].text
		p.pos += 4
		return &aForIn{v: v, arr: arr, body: p.simpleOrBlock()}
	}
	f := &aFor{}
	if !p.isOp(";") {
		f.init = &aExprStmt{x: p.expr()}
	}
	p.expectOp(";")
	p.skipNL()
	if !p.isOp(";") {
		f.c = p.expr()
	}
	p.expectOp(";")
	p.skipNL()
	if !p.isOp(")") {
		f.post = &aExprStmt{x: p.expr()}
	}
	p.expectOp(")")
	if p.isOp(";") {
		p.pos++
		f.body = aBlock{}
		return f
	}
	f.body = p.simpleOrBlock()
	return f
}

func (p *awkParser) printStmt() awkStmt {
	s := &aPrint{printf: p.peek().text == "printf"}
	p.pos++
	p.noGT++
	if !p.isOp(";") && !p.isOp("}") && !p.isOp(">") && !p.isOp(">>") && !p.isOp("|") && p.peek().kind != "nl" && p.peek().kind != "eof" {
		s.args = p.exprList("")
	}
	p.noGT--
	if len(s.args) == 1 {
		if g, ok := s.args[0].(*aGroup); ok {
			s.args = g.list
		}
	}
	switch {
	case p.isOp(">") || p.isOp(">>"):
		s.append = p.isOp(">>")
		p.pos++
		s.dest = p.concat()
	case p.isOp("|"):
		p.fail("print to a command is not supported by the in-browser shell")
	}
	p.endStmt()
	return s
}

func (p *awkParser) exprList(closer string) []awkExpr {
	var list []awkExpr
	for {
		list = append(list, p.expr())
		if !p.isOp(",") {
			break
		}
		p.pos++
		p.skipNL()
	}
	if closer != "" {
		p.expectOp(closer)
	}
	return list
}
