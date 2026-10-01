package wasmshell

import (
	"strings"
)

func (e *jqEnv) binary(n *jqBinary, in any) ([]any, error) {
	switch n.op {
	case "//":
		l, err := e.eval(n.l, in)
		var out []any
		if err == nil {
			for _, v := range l {
				if jqTruthy(v) {
					out = append(out, v)
				}
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		return e.eval(n.r, in)
	case "and", "or":
		l, err := e.eval(n.l, in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, lv := range l {
			if n.op == "and" && !jqTruthy(lv) || n.op == "or" && jqTruthy(lv) {
				out = append(out, n.op == "or")
				continue
			}
			r, err := e.eval(n.r, in)
			if err != nil {
				return nil, err
			}
			for _, rv := range r {
				out = append(out, jqTruthy(rv))
			}
		}
		return out, nil
	}
	r, err := e.eval(n.r, in)
	if err != nil {
		return nil, err
	}
	l, err := e.eval(n.l, in)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, rv := range r {
		for _, lv := range l {
			v, err := jqArith(n.op, lv, rv)
			if err != nil {
				return out, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func jqArith(op string, a, b any) (any, error) {
	switch op {
	case "==":
		return jqCompare(a, b) == 0, nil
	case "!=":
		return jqCompare(a, b) != 0, nil
	case "<":
		return jqCompare(a, b) < 0, nil
	case "<=":
		return jqCompare(a, b) <= 0, nil
	case ">":
		return jqCompare(a, b) > 0, nil
	case ">=":
		return jqCompare(a, b) >= 0, nil
	}
	x, xNum := a.(float64)
	y, yNum := b.(float64)
	switch op {
	case "+":
		switch {
		case a == nil:
			return b, nil
		case b == nil:
			return a, nil
		case xNum && yNum:
			return x + y, nil
		}
		switch av := a.(type) {
		case string:
			if bs, ok := b.(string); ok {
				return av + bs, nil
			}
		case []any:
			if bs, ok := b.([]any); ok {
				return append(append([]any{}, av...), bs...), nil
			}
		case *jqObj:
			if bo, ok := b.(*jqObj); ok {
				o := av.clone()
				for _, k := range bo.keys {
					o.set(k, bo.m[k])
				}
				return o, nil
			}
		}
	case "-":
		if xNum && yNum {
			return x - y, nil
		}
		if av, ok := a.([]any); ok {
			if bv, ok := b.([]any); ok {
				var out []any
				for _, v := range av {
					keep := true
					for _, r := range bv {
						if jqCompare(v, r) == 0 {
							keep = false
						}
					}
					if keep {
						out = append(out, v)
					}
				}
				if out == nil {
					out = []any{}
				}
				return out, nil
			}
		}
	case "*":
		if xNum && yNum {
			return x * y, nil
		}
		if s, ok := a.(string); ok && yNum {
			if y <= 0 {
				return nil, nil
			}
			return strings.Repeat(s, int(y)), nil
		}
		if ao, ok := a.(*jqObj); ok {
			if bo, ok := b.(*jqObj); ok {
				return jqDeepMerge(ao, bo), nil
			}
		}
	case "/":
		if xNum && yNum {
			if y == 0 {
				return nil, jqErrf("%s and %s cannot be divided because the divisor is zero", jqDescribe(a), jqDescribe(b))
			}
			return x / y, nil
		}
		if s, ok := a.(string); ok {
			if sep, ok := b.(string); ok {
				return toAnySlice(strings.Split(s, sep)), nil
			}
		}
	case "%":
		if xNum && yNum {
			if int64(y) == 0 {
				return nil, jqErrf("%s and %s cannot be divided because the divisor is zero", jqDescribe(a), jqDescribe(b))
			}
			return float64(int64(x) % int64(y)), nil
		}
	}
	return nil, jqErrf("%s and %s cannot be %s", jqDescribe(a), jqDescribe(b), map[string]string{
		"+": "added", "-": "subtracted", "*": "multiplied", "/": "divided", "%": "divided"}[op])
}

func jqDeepMerge(a, b *jqObj) *jqObj {
	o := a.clone()
	for _, k := range b.keys {
		if ao, ok := o.m[k].(*jqObj); ok {
			if bo, ok := b.m[k].(*jqObj); ok {
				o.set(k, jqDeepMerge(ao, bo))
				continue
			}
		}
		o.set(k, b.m[k])
	}
	return o
}

func jqWalkValues(v any, fn func(any)) {
	fn(v)
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			jqWalkValues(x, fn)
		}
	case *jqObj:
		for _, k := range t.keys {
			jqWalkValues(t.m[k], fn)
		}
	}
}

func jqEnvObject() *jqObj {
	o := newJqObj()
	for _, k := range sortedKeys(ShellEnv.All()) {
		o.set(k, ShellEnv.Get(k))
	}
	return o
}
