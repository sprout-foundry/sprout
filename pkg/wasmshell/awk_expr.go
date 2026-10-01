package wasmshell

import "regexp"

func (p *awkParser) expr() awkExpr { return p.ternary() }

func isLvalue(x awkExpr) bool {
	switch x.(type) {
	case *aVar, *aField, *aIndex:
		return true
	}
	return false
}

var awkAssignOps = words("=", "+=", "-=", "*=", "/=", "%=", "^=", "**=")

func (p *awkParser) ternary() awkExpr {
	c := p.or()
	if p.isOp("?") {
		p.pos++
		p.skipNL()
		a := p.ternary()
		p.skipNL()
		p.expectOp(":")
		p.skipNL()
		return &aCond{c: c, a: a, b: p.ternary()}
	}
	if t := p.peek(); t.kind == "op" && awkAssignOps[t.text] && isLvalue(c) {
		p.pos++
		p.skipNL()
		return &aAssign{op: t.text, lhs: c, rhs: p.ternary()}
	}
	return c
}

func (p *awkParser) or() awkExpr {
	x := p.and()
	for p.isOp("||") {
		p.pos++
		p.skipNL()
		x = &aBinary{op: "||", l: x, r: p.and()}
	}
	return x
}

func (p *awkParser) and() awkExpr {
	x := p.in()
	for p.isOp("&&") {
		p.pos++
		p.skipNL()
		x = &aBinary{op: "&&", l: x, r: p.in()}
	}
	return x
}

func (p *awkParser) in() awkExpr {
	x := p.match()
	for p.isKw("in") && p.noIn == 0 {
		p.pos++
		name := p.peek()
		if name.kind != "name" {
			p.fail("expected array after in")
		}
		p.pos++
		keys := []awkExpr{x}
		if g, ok := x.(*aGroup); ok {
			keys = g.list
		}
		x = &aIn{keys: keys, arr: name.text}
	}
	return x
}

func (p *awkParser) match() awkExpr {
	x := p.comparison()
	for p.isOp("~") || p.isOp("!~") {
		op := p.peek().text
		p.pos++
		x = &aBinary{op: op, l: x, r: p.comparison()}
	}
	return x
}

func (p *awkParser) comparison() awkExpr {
	x := p.concat()
	t := p.peek()
	if t.kind == "op" {
		switch t.text {
		case "<", "<=", "!=", "==", ">=":
			p.pos++
			return &aBinary{op: t.text, l: x, r: p.concat()}
		case ">":
			if p.noGT == 0 {
				p.pos++
				return &aBinary{op: t.text, l: x, r: p.concat()}
			}
		}
	}
	return x
}

func (p *awkParser) startsOperand() bool {
	t := p.peek()
	switch t.kind {
	case "num", "str", "re", "name", "func":
		return true
	case "op":
		return t.text == "$" || t.text == "(" || t.text == "!" || t.text == "++" || t.text == "--" || t.text == "-"
	}
	return false
}

func (p *awkParser) concat() awkExpr {
	x := p.additive()
	for {
		t := p.peek()
		if t.kind == "op" && (t.text == "-" || t.text == "!") {
			return x
		}
		if !p.startsOperand() || (t.kind == "kw") {
			return x
		}
		x = &aBinary{op: "concat", l: x, r: p.additive()}
	}
}

func (p *awkParser) additive() awkExpr {
	x := p.mul()
	for p.isOp("+") || p.isOp("-") {
		op := p.peek().text
		p.pos++
		x = &aBinary{op: op, l: x, r: p.mul()}
	}
	return x
}

func (p *awkParser) mul() awkExpr {
	x := p.unary()
	for p.isOp("*") || p.isOp("/") || p.isOp("%") {
		op := p.peek().text
		p.pos++
		x = &aBinary{op: op, l: x, r: p.unary()}
	}
	return x
}

func (p *awkParser) unary() awkExpr {
	switch {
	case p.isOp("!"):
		p.pos++
		return &aUnary{op: "!", x: p.unary()}
	case p.isOp("-"):
		p.pos++
		return &aUnary{op: "-", x: p.unary()}
	case p.isOp("+"):
		p.pos++
		return &aUnary{op: "+", x: p.unary()}
	}
	return p.pow()
}

func (p *awkParser) pow() awkExpr {
	x := p.postfix()
	if p.isOp("^") || p.isOp("**") {
		p.pos++
		return &aBinary{op: "^", l: x, r: p.unary()}
	}
	return x
}

func (p *awkParser) postfix() awkExpr {
	x := p.primary()
	if (p.isOp("++") || p.isOp("--")) && isLvalue(x) {
		op := p.peek().text
		p.pos++
		return &aIncDec{op: op, lv: x}
	}
	return x
}

func (p *awkParser) primary() awkExpr {
	t := p.peek()
	switch t.kind {
	case "num":
		p.pos++
		return &aNum{v: t.num}
	case "str":
		p.pos++
		return &aStr{v: t.text}
	case "re":
		p.pos++
		re, err := regexp.Compile(t.text)
		if err != nil {
			p.fail("bad regex /" + t.text + "/")
		}
		return &aRegex{re: re}
	case "func":
		p.pos++
		c := &aCall{name: t.text}
		if p.isOp("(") {
			p.pos++
			if !p.isOp(")") {
				p.noGT, p.noIn = 0, 0
				c.args = p.exprList("")
			}
			p.expectOp(")")
		} else if t.text != "length" {
			p.fail("expected ( after " + t.text)
		}
		return c
	case "name":
		p.pos++
		if p.isOp("(") {
			p.pos++
			c := &aCall{name: t.text}
			if !p.isOp(")") {
				c.args = p.exprList("")
			}
			p.expectOp(")")
			return c
		}
		if p.isOp("[") {
			p.pos++
			return &aIndex{name: t.text, keys: p.exprList("]")}
		}
		return &aVar{name: t.text}
	case "kw":
		if t.text == "getline" {
			p.fail("getline is not supported by the in-browser shell")
		}
	case "op":
		switch t.text {
		case "$":
			p.pos++
			if p.isOp("++") || p.isOp("--") {
				op := p.peek().text
				p.pos++
				return &aField{idx: &aIncDec{op: op, pre: true, lv: p.primary()}}
			}
			if p.isOp("-") {
				p.pos++
				return &aField{idx: &aUnary{op: "-", x: p.primary()}}
			}
			return &aField{idx: p.primary()}
		case "(":
			p.pos++
			saveGT, saveIn := p.noGT, p.noIn
			p.noGT, p.noIn = 0, 0
			list := p.exprList(")")
			p.noGT, p.noIn = saveGT, saveIn
			if len(list) == 1 {
				return list[0]
			}
			return &aGroup{list: list}
		case "++", "--":
			p.pos++
			lv := p.primary()
			if !isLvalue(lv) {
				p.fail("++ or -- needs a variable")
			}
			return &aIncDec{op: t.text, pre: true, lv: lv}
		case "-", "!", "+":
			return p.unary()
		}
	}
	p.fail("unexpected token")
	return nil
}
