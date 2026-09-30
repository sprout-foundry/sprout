package wasmshell

import (
	"fmt"
	"math"
	"strings"
)

const awkMaxLoop = 10_000_000

func (r *awkRun) runBlock(b aBlock) awkSignal {
	for _, s := range b {
		if sig := r.exec(s); sig != sigNone {
			return sig
		}
	}
	return sigNone
}

func (r *awkRun) exec(s awkStmt) awkSignal {
	switch s := s.(type) {
	case aBlock:
		return r.runBlock(s)
	case *aExprStmt:
		r.eval(s.x)
	case *aPrint:
		r.execPrint(s)
	case *aIf:
		if r.truthy(r.eval(s.c)) {
			return r.exec(s.then)
		} else if s.els != nil {
			return r.exec(s.els)
		}
	case *aWhile:
		for n := 0; ; n++ {
			if n > awkMaxLoop {
				panic(awkRuntimeError{"loop iteration limit reached"})
			}
			if (!s.do || n != 0) && !r.truthy(r.eval(s.c)) {
				break
			}
			sig := r.exec(s.body)
			if sig == sigBreak {
				break
			}
			if sig != sigNone && sig != sigContinue {
				return sig
			}
		}
	case *aFor:
		if s.init != nil {
			r.exec(s.init)
		}
		for n := 0; ; n++ {
			if n > awkMaxLoop {
				panic(awkRuntimeError{"loop iteration limit reached"})
			}
			if s.c != nil && !r.truthy(r.eval(s.c)) {
				break
			}
			sig := r.exec(s.body)
			if sig == sigBreak {
				break
			}
			if sig != sigNone && sig != sigContinue {
				return sig
			}
			if s.post != nil {
				r.exec(s.post)
			}
		}
	case *aForIn:
		arr := r.array(s.arr)
		for _, k := range r.sortedKeys(arr) {
			if _, still := arr[k]; !still {
				continue
			}
			r.setVar(s.v, strnum(k))
			sig := r.exec(s.body)
			if sig == sigBreak {
				break
			}
			if sig != sigNone && sig != sigContinue {
				return sig
			}
		}
	case aNext:
		return sigNext
	case aBreak:
		return sigBreak
	case aCont:
		return sigContinue
	case *aExit:
		if s.code != nil {
			r.exitCode = int(r.toNum(r.eval(s.code)))
		}
		r.exiting = true
		return sigExit
	case *aReturn:
		if len(r.frames) == 0 {
			panic(awkRuntimeError{"return outside function"})
		}
		if s.x != nil {
			r.frames[len(r.frames)-1].vars["\x00ret"] = r.eval(s.x)
		}
		return sigReturn
	case *aDelete:
		arr := r.array(s.name)
		if s.keys == nil {
			for k := range arr {
				delete(arr, k)
			}
		} else {
			delete(arr, r.key(s.keys))
		}
	}
	return sigNone
}

func (r *awkRun) execPrint(s *aPrint) {
	var text string
	if s.printf {
		if len(s.args) == 0 {
			panic(awkRuntimeError{"printf: no format"})
		}
		vals := make([]aval, len(s.args)-1)
		for k, a := range s.args[1:] {
			vals[k] = r.eval(a)
		}
		text = r.sprintf(r.toStr(r.eval(s.args[0])), vals)
	} else {
		if len(s.args) == 0 {
			text = r.record
		} else {
			parts := make([]string, len(s.args))
			for k, a := range s.args {
				v := r.eval(a)
				if v.kind == kNum {
					parts[k] = r.numToStr(v.n, "OFMT")
				} else {
					parts[k] = v.s
				}
			}
			text = strings.Join(parts, r.getStr("OFS"))
		}
		text += r.getStr("ORS")
	}
	r.emit(text, s.dest, s.append)
}

func (r *awkRun) frame() *awkFrame {
	if len(r.frames) == 0 {
		return nil
	}
	return r.frames[len(r.frames)-1]
}

func (r *awkRun) getVar(name string) aval {
	if f := r.frame(); f != nil {
		if v, ok := f.vars[name]; ok {
			return v
		}
	}
	if name == "NF" && !r.fieldsOK {
		r.splitFields()
	}
	if v, ok := r.globals[name]; ok {
		return v
	}
	return aval{kind: kStrNum}
}

func (r *awkRun) setVar(name string, v aval) {
	if f := r.frame(); f != nil {
		if _, ok := f.vars[name]; ok {
			f.vars[name] = v
			return
		}
	}
	r.globals[name] = v
	if name == "NF" {
		n := int(r.toNum(v))
		if n < len(r.fields) {
			r.fields = r.fields[:max(n, 0)]
		}
		for len(r.fields) < n {
			r.fields = append(r.fields, "")
		}
		r.rebuildRecord()
	}
}

func (r *awkRun) array(name string) map[string]aval {
	if f := r.frame(); f != nil {
		if a, ok := f.arrays[name]; ok {
			return a
		}
		if _, ok := f.vars[name]; ok {
			a := map[string]aval{}
			f.arrays[name] = a
			delete(f.vars, name)
			return a
		}
	}
	a, ok := r.arrays[name]
	if !ok {
		a = map[string]aval{}
		r.arrays[name] = a
	}
	return a
}

func (r *awkRun) key(keys []awkExpr) string {
	parts := make([]string, len(keys))
	for k, e := range keys {
		parts[k] = r.toStr(r.eval(e))
	}
	return strings.Join(parts, r.getStr("SUBSEP"))
}

func (r *awkRun) field(i int) aval {
	if i < 0 {
		panic(awkRuntimeError{fmt.Sprintf("trying to access out of range field %d", i)})
	}
	if i == 0 {
		return strnum(r.record)
	}
	if i > len(r.fields) {
		return aval{kind: kStrNum}
	}
	return strnum(r.fields[i-1])
}

func (r *awkRun) setField(i int, s string) {
	if i < 0 {
		panic(awkRuntimeError{fmt.Sprintf("trying to access out of range field %d", i)})
	}
	if i == 0 {
		r.setRecord(s)
		return
	}
	for len(r.fields) < i {
		r.fields = append(r.fields, "")
	}
	r.fields[i-1] = s
	r.globals["NF"] = numVal(float64(len(r.fields)))
	r.rebuildRecord()
}

func (r *awkRun) assign(lv awkExpr, v aval) aval {
	switch lv := lv.(type) {
	case *aVar:
		r.setVar(lv.name, v)
	case *aField:
		r.setField(int(r.toNum(r.eval(lv.idx))), r.toStr(v))
	case *aIndex:
		r.array(lv.name)[r.key(lv.keys)] = v
	}
	return v
}

func (r *awkRun) matchRe(x awkExpr) func(string) bool {
	if re, ok := x.(*aRegex); ok {
		return re.re.MatchString
	}
	re := r.regex(r.toStr(r.eval(x)))
	return re.MatchString
}

func (r *awkRun) compare(op string, a, b aval) bool {
	var c int
	numeric := func(v aval) bool { return v.kind == kNum || (v.kind == kStrNum && (v.s == "" || looksNumeric(v.s))) }
	if numeric(a) && numeric(b) {
		x, y := r.toNum(a), r.toNum(b)
		switch {
		case x < y:
			c = -1
		case x > y:
			c = 1
		}
	} else {
		c = strings.Compare(r.toStr(a), r.toStr(b))
	}
	switch op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	case "==":
		return c == 0
	}
	return c != 0
}

func awkBool(b bool) aval {
	if b {
		return numVal(1)
	}
	return numVal(0)
}

func (r *awkRun) eval(x awkExpr) aval {
	switch x := x.(type) {
	case *aNum:
		return numVal(x.v)
	case *aStr:
		return strVal(x.v)
	case *aRegex:
		return awkBool(x.re.MatchString(r.record))
	case *aVar:
		return r.getVar(x.name)
	case *aField:
		return r.field(int(r.toNum(r.eval(x.idx))))
	case *aIndex:
		arr := r.array(x.name)
		k := r.key(x.keys)
		v, ok := arr[k]
		if !ok {
			v = aval{kind: kStrNum}
			arr[k] = v
		}
		return v
	case *aGroup:
		return r.eval(x.list[len(x.list)-1])
	case *aIn:
		_, ok := r.array(x.arr)[r.key(x.keys)]
		return awkBool(ok)
	case *aCond:
		if r.truthy(r.eval(x.c)) {
			return r.eval(x.a)
		}
		return r.eval(x.b)
	case *aAssign:
		v := r.eval(x.rhs)
		if x.op != "=" {
			cur := r.toNum(r.eval(x.lhs))
			v = numVal(arith(strings.TrimSuffix(x.op, "="), cur, r.toNum(v)))
		} else if v.kind == kStrNum && v.s == "" {
			v = aval{kind: kStrNum}
		}
		return r.assign(x.lhs, v)
	case *aIncDec:
		cur := r.toNum(r.eval(x.lv))
		next := cur + 1
		if x.op == "--" {
			next = cur - 1
		}
		r.assign(x.lv, numVal(next))
		if x.pre {
			return numVal(next)
		}
		return numVal(cur)
	case *aUnary:
		v := r.eval(x.x)
		switch x.op {
		case "!":
			return awkBool(!r.truthy(v))
		case "-":
			return numVal(-r.toNum(v))
		}
		return numVal(r.toNum(v))
	case *aBinary:
		switch x.op {
		case "&&":
			return awkBool(r.truthy(r.eval(x.l)) && r.truthy(r.eval(x.r)))
		case "||":
			return awkBool(r.truthy(r.eval(x.l)) || r.truthy(r.eval(x.r)))
		case "~", "!~":
			s := r.toStr(r.eval(x.l))
			return awkBool(r.matchRe(x.r)(s) == (x.op == "~"))
		case "concat":
			return strVal(r.toStr(r.eval(x.l)) + r.toStr(r.eval(x.r)))
		case "<", "<=", ">", ">=", "==", "!=":
			return awkBool(r.compare(x.op, r.eval(x.l), r.eval(x.r)))
		}
		return numVal(arith(x.op, r.toNum(r.eval(x.l)), r.toNum(r.eval(x.r))))
	case *aCall:
		return r.call(x)
	}
	panic(awkRuntimeError{fmt.Sprintf("cannot evaluate %T", x)})
}

func arith(op string, a, b float64) float64 {
	switch op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		if b == 0 {
			panic(awkRuntimeError{"division by zero"})
		}
		return a / b
	case "%":
		if b == 0 {
			panic(awkRuntimeError{"division by zero in %"})
		}
		return math.Mod(a, b)
	case "^", "**":
		return math.Pow(a, b)
	}
	return 0
}
