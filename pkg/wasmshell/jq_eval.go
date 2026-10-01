package wasmshell

import (
	"fmt"
	"math"
)

type jqEnv struct {
	vars map[string]any
}

func (e *jqEnv) with(name string, v any) *jqEnv {
	vars := make(map[string]any, len(e.vars)+1)
	for k, x := range e.vars {
		vars[k] = x
	}
	vars[name] = v
	return &jqEnv{vars: vars}
}

func jqErrf(format string, args ...any) error { return jqError{fmt.Sprintf(format, args...)} }

func (e *jqEnv) eval(n jqNode, in any) ([]any, error) {
	switch n := n.(type) {
	case jqIdentity:
		return []any{in}, nil
	case jqRecurse:
		var out []any
		jqWalkValues(in, func(v any) { out = append(out, v) })
		return out, nil
	case *jqLiteral:
		return []any{n.v}, nil
	case *jqVarRef:
		if n.name == "ENV" || n.name == "__loc__" {
			return []any{jqEnvObject()}, nil
		}
		v, ok := e.vars[n.name]
		if !ok {
			return nil, jqErrf("$%s is not defined", n.name)
		}
		return []any{v}, nil
	case *jqField:
		return e.each(n.target, in, func(t any) ([]any, error) {
			v, err := jqIndexValue(t, n.name)
			if err != nil && n.opt {
				return nil, nil
			}
			return []any{v}, err
		})
	case *jqIndex:
		return e.each(n.target, in, func(t any) ([]any, error) {
			idxs, err := e.eval(n.index, in)
			if err != nil {
				return nil, err
			}
			var out []any
			for _, idx := range idxs {
				v, err := jqIndexValue(t, idx)
				if err != nil {
					if n.opt {
						continue
					}
					return nil, err
				}
				out = append(out, v)
			}
			return out, nil
		})
	case *jqSlice:
		return e.each(n.target, in, func(t any) ([]any, error) { return e.slice(n, t, in) })
	case *jqIterate:
		return e.each(n.target, in, func(t any) ([]any, error) {
			switch v := t.(type) {
			case []any:
				return v, nil
			case *jqObj:
				out := make([]any, 0, len(v.keys))
				for _, k := range v.keys {
					out = append(out, v.m[k])
				}
				return out, nil
			}
			if n.opt {
				return nil, nil
			}
			return nil, jqErrf("Cannot iterate over %s", jqDescribe(t))
		})
	case *jqPipe:
		left, err := e.eval(n.l, in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, v := range left {
			r, err := e.eval(n.r, v)
			if err != nil {
				return out, err
			}
			out = append(out, r...)
		}
		return out, nil
	case *jqComma:
		l, err := e.eval(n.l, in)
		if err != nil {
			return l, err
		}
		r, err := e.eval(n.r, in)
		return append(l, r...), err
	case *jqAs:
		srcs, err := e.eval(n.src, in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, v := range srcs {
			r, err := e.with(n.name, v).eval(n.body, in)
			if err != nil {
				return out, err
			}
			out = append(out, r...)
		}
		return out, nil
	case *jqReduce:
		accs, err := e.eval(n.init, in)
		if err != nil || len(accs) == 0 {
			return nil, err
		}
		acc := accs[len(accs)-1]
		srcs, err := e.eval(n.src, in)
		if err != nil {
			return nil, err
		}
		for _, v := range srcs {
			r, err := e.with(n.name, v).eval(n.upd, acc)
			if err != nil {
				return nil, err
			}
			if len(r) == 0 {
				acc = nil
				continue
			}
			acc = r[len(r)-1]
		}
		return []any{acc}, nil
	case *jqNeg:
		vals, err := e.eval(n.x, in)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(vals))
		for _, v := range vals {
			f, ok := v.(float64)
			if !ok {
				return nil, jqErrf("%s cannot be negated", jqDescribe(v))
			}
			out = append(out, -f)
		}
		return out, nil
	case *jqArray:
		if n.body == nil {
			return []any{[]any{}}, nil
		}
		vals, err := e.eval(n.body, in)
		if err != nil {
			return nil, err
		}
		if vals == nil {
			vals = []any{}
		}
		return []any{vals}, nil
	case *jqObject:
		return e.object(n, in)
	case *jqIf:
		return e.ifExpr(n, 0, in)
	case *jqTry:
		out, err := e.eval(n.x, in)
		if err != nil {
			return out, nil
		}
		return out, nil
	case *jqStringNode:
		return e.interpolate(n.parts, in)
	case *jqFormat:
		s, err := jqFormatValue(n.name, in)
		if err != nil {
			return nil, err
		}
		return []any{s}, nil
	case *jqBinary:
		return e.binary(n, in)
	case *jqCall:
		return e.call(n, in)
	}
	return nil, jqErrf("unsupported expression")
}

// each evaluates target and applies fn to every result.
func (e *jqEnv) each(target jqNode, in any, fn func(any) ([]any, error)) ([]any, error) {
	ts, err := e.eval(target, in)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, t := range ts {
		r, err := fn(t)
		if err != nil {
			return out, err
		}
		out = append(out, r...)
	}
	return out, nil
}

func jqDescribe(v any) string {
	s := encodeJSON(v, "", false)
	if len(s) > 11 {
		s = s[:10] + "..."
	}
	return jqTypeName(v) + " (" + s + ")"
}

func jqIndexValue(t, idx any) (any, error) {
	switch v := t.(type) {
	case nil:
		return nil, nil
	case *jqObj:
		k, ok := idx.(string)
		if !ok {
			return nil, jqErrf("Cannot index object with %s", jqTypeName(idx))
		}
		return v.m[k], nil
	case []any:
		f, ok := idx.(float64)
		if !ok {
			return nil, jqErrf("Cannot index array with %s", jqTypeName(idx))
		}
		i := int(math.Floor(f))
		if i < 0 {
			i += len(v)
		}
		if i < 0 || i >= len(v) {
			return nil, nil
		}
		return v[i], nil
	}
	if s, ok := idx.(string); ok {
		return nil, jqErrf("Cannot index %s with \"%s\"", jqTypeName(t), s)
	}
	return nil, jqErrf("Cannot index %s with %s", jqTypeName(t), jqTypeName(idx))
}

func (e *jqEnv) slice(n *jqSlice, t, in any) ([]any, error) {
	bound := func(node jqNode, def int, length int) (int, error) {
		if node == nil {
			return def, nil
		}
		vs, err := e.eval(node, in)
		if err != nil || len(vs) == 0 {
			return def, err
		}
		if vs[0] == nil {
			return def, nil
		}
		f, ok := vs[0].(float64)
		if !ok {
			return 0, jqErrf("slice indices must be numbers")
		}
		i := int(math.Floor(f))
		if i < 0 {
			i += length
		}
		return max(0, min(i, length)), nil
	}
	var length int
	switch v := t.(type) {
	case nil:
		return []any{nil}, nil
	case []any:
		length = len(v)
	case string:
		length = len([]rune(v))
	default:
		return nil, jqErrf("Cannot index %s with object", jqTypeName(t))
	}
	from, err := bound(n.from, 0, length)
	if err != nil {
		return nil, err
	}
	to, err := bound(n.to, length, length)
	if err != nil {
		return nil, err
	}
	if to < from {
		to = from
	}
	if s, ok := t.(string); ok {
		return []any{string([]rune(s)[from:to])}, nil
	}
	return []any{append([]any{}, t.([]any)[from:to]...)}, nil
}

func (e *jqEnv) object(n *jqObject, in any) ([]any, error) {
	results := []*jqObj{newJqObj()}
	for _, ent := range n.entries {
		keys := []any{ent.keyName}
		if ent.keyExpr != nil {
			var err error
			if keys, err = e.eval(ent.keyExpr, in); err != nil {
				return nil, err
			}
		}
		vals, err := e.eval(ent.value, in)
		if err != nil {
			return nil, err
		}
		var next []*jqObj
		for _, base := range results {
			for _, k := range keys {
				ks, ok := k.(string)
				if !ok {
					return nil, jqErrf("Object keys must be strings")
				}
				for _, v := range vals {
					o := base.clone()
					o.set(ks, v)
					next = append(next, o)
				}
			}
		}
		results = next
	}
	out := make([]any, len(results))
	for i, o := range results {
		out[i] = o
	}
	return out, nil
}

func (e *jqEnv) ifExpr(n *jqIf, i int, in any) ([]any, error) {
	if i >= len(n.conds) {
		if n.els == nil {
			return []any{in}, nil
		}
		return e.eval(n.els, in)
	}
	conds, err := e.eval(n.conds[i], in)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, c := range conds {
		var r []any
		if jqTruthy(c) {
			r, err = e.eval(n.thens[i], in)
		} else {
			r, err = e.ifExpr(n, i+1, in)
		}
		if err != nil {
			return out, err
		}
		out = append(out, r...)
	}
	return out, nil
}

func (e *jqEnv) interpolate(parts []jqStrPart, in any) ([]any, error) {
	results := []string{""}
	for _, p := range parts {
		if p.expr == nil {
			for i := range results {
				results[i] += p.lit
			}
			continue
		}
		vals, err := e.eval(p.expr, in)
		if err != nil {
			return nil, err
		}
		var next []string
		for _, r := range results {
			for _, v := range vals {
				next = append(next, r+jqToString(v))
			}
		}
		results = next
	}
	out := make([]any, len(results))
	for i, s := range results {
		out[i] = s
	}
	return out, nil
}
