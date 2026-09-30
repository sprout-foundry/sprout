package wasmshell

import (
	"sort"
)

func jqValuesOf(in any) ([]any, error) {
	switch v := in.(type) {
	case []any:
		return v, nil
	case *jqObj:
		out := make([]any, 0, len(v.keys))
		for _, k := range v.keys {
			out = append(out, v.m[k])
		}
		return out, nil
	case nil:
		return nil, nil
	}
	return nil, jqErrf("Cannot iterate over %s", jqDescribe(in))
}

func jqQuantify(name string, arr []any, pred func(any) bool) bool {
	for _, v := range arr {
		if name == "any" && pred(v) {
			return true
		}
		if name == "all" && !pred(v) {
			return false
		}
	}
	return name == "all"
}

func jqFlatten(arr []any, depth float64) []any {
	out := []any{}
	for _, v := range arr {
		if sub, ok := v.([]any); ok && depth > 0 {
			out = append(out, jqFlatten(sub, depth-1)...)
			continue
		}
		out = append(out, v)
	}
	return out
}

func jqPaths(v any, prefix []any, fn func([]any, any)) {
	switch t := v.(type) {
	case []any:
		for i, x := range t {
			p := append(prefix, float64(i))
			fn(p, x)
			jqPaths(x, p, fn)
		}
	case *jqObj:
		for _, k := range t.keys {
			p := append(prefix, k)
			fn(p, t.m[k])
			jqPaths(t.m[k], p, fn)
		}
	}
}

func (e *jqEnv) walk(f jqNode, v any) (any, error) {
	switch t := v.(type) {
	case []any:
		out := make([]any, 0, len(t))
		for _, x := range t {
			w, err := e.walk(f, x)
			if err != nil {
				return nil, err
			}
			out = append(out, w)
		}
		v = out
	case *jqObj:
		o := newJqObj()
		for _, k := range t.keys {
			w, err := e.walk(f, t.m[k])
			if err != nil {
				return nil, err
			}
			o.set(k, w)
		}
		v = o
	}
	r, err := e.eval(f, v)
	if err != nil || len(r) == 0 {
		return nil, err
	}
	return r[0], nil
}

func jqArrayFn(name string, in any) ([]any, error) {
	arr, ok := in.([]any)
	if !ok {
		if name == "reverse" {
			if s, isStr := in.(string); isStr {
				rs := []rune(s)
				for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
					rs[i], rs[j] = rs[j], rs[i]
				}
				return []any{string(rs)}, nil
			}
			if in == nil {
				return []any{[]any{}}, nil
			}
		}
		return nil, jqErrf("%s cannot be sorted, as it is not an array", jqDescribe(in))
	}
	cp := append([]any{}, arr...)
	switch name {
	case "sort":
		sort.SliceStable(cp, func(a, b int) bool { return jqCompare(cp[a], cp[b]) < 0 })
	case "unique":
		sort.SliceStable(cp, func(a, b int) bool { return jqCompare(cp[a], cp[b]) < 0 })
		var out []any
		for i, v := range cp {
			if i == 0 || jqCompare(cp[i-1], v) != 0 {
				out = append(out, v)
			}
		}
		if out == nil {
			out = []any{}
		}
		cp = out
	case "reverse":
		for i, j := 0, len(cp)-1; i < j; i, j = i+1, j-1 {
			cp[i], cp[j] = cp[j], cp[i]
		}
	case "min", "max", "first", "last":
		if len(cp) == 0 {
			return []any{nil}, nil
		}
		best := cp[0]
		switch name {
		case "last":
			best = cp[len(cp)-1]
		case "min", "max":
			for _, v := range cp[1:] {
				c := jqCompare(v, best)
				if (name == "min" && c < 0) || (name == "max" && c >= 0) {
					best = v
				}
			}
		}
		return []any{best}, nil
	}
	return []any{cp}, nil
}

func (e *jqEnv) byFn(c *jqCall, in any) ([]any, error) {
	arr, ok := in.([]any)
	if !ok {
		return nil, jqErrf("Cannot index %s", jqDescribe(in))
	}
	keys := make([]any, len(arr))
	for i, v := range arr {
		r, err := e.eval(&jqArray{body: c.args[0]}, v)
		if err != nil {
			return nil, err
		}
		keys[i] = r[0]
	}
	idx := make([]int, len(arr))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return jqCompare(keys[idx[a]], keys[idx[b]]) < 0 })
	switch c.name {
	case "sort_by":
		out := make([]any, len(arr))
		for i, k := range idx {
			out[i] = arr[k]
		}
		return []any{out}, nil
	case "min_by", "max_by":
		if len(arr) == 0 {
			return []any{nil}, nil
		}
		if c.name == "min_by" {
			return []any{arr[idx[0]]}, nil
		}
		return []any{arr[idx[len(idx)-1]]}, nil
	}
	var groups []any
	var cur []any
	for i, k := range idx {
		if i > 0 && jqCompare(keys[idx[i-1]], keys[k]) != 0 {
			groups = append(groups, cur)
			cur = nil
		}
		cur = append(cur, arr[k])
	}
	if cur != nil {
		groups = append(groups, cur)
	}
	if c.name == "unique_by" {
		for i, g := range groups {
			groups[i] = g.([]any)[0]
		}
	}
	if groups == nil {
		groups = []any{}
	}
	return []any{groups}, nil
}

func (e *jqEnv) takeFn(c *jqCall, in any, arg func(int) (any, error)) ([]any, error) {
	var n int
	body := c.args[0]
	if c.name == "limit" {
		v, err := arg(0)
		if err != nil {
			return nil, err
		}
		f, _ := v.(float64)
		n, body = int(f), c.args[1]
	}
	vals, err := e.eval(body, in)
	switch c.name {
	case "first":
		if len(vals) == 0 {
			return nil, err
		}
		return vals[:1], nil
	case "last":
		if len(vals) == 0 {
			return nil, err
		}
		return vals[len(vals)-1:], err
	}
	if n < len(vals) {
		return vals[:max(n, 0)], nil
	}
	return vals, err
}
