package wasmshell

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (e *jqEnv) call(c *jqCall, in any) ([]any, error) {
	one := func(v any) ([]any, error) { return []any{v}, nil }
	argVals := func(k int) ([]any, error) { return e.eval(c.args[k], in) }
	arg := func(k int) (any, error) {
		vs, err := argVals(k)
		if err != nil {
			return nil, err
		}
		if len(vs) == 0 {
			return nil, jqErrf("%s: argument produced no value", c.name)
		}
		return vs[0], nil
	}
	arity := len(c.args)
	switch fmt.Sprintf("%s/%d", c.name, arity) {
	case "empty/0":
		return nil, nil
	case "error/0":
		return nil, jqErrf("%s", jqToString(in))
	case "error/1":
		v, err := arg(0)
		if err != nil {
			return nil, err
		}
		return nil, jqErrf("%s", jqToString(v))
	case "not/0":
		return one(!jqTruthy(in))
	case "length/0":
		switch v := in.(type) {
		case nil:
			return one(0.0)
		case bool:
			return nil, jqErrf("boolean (%v) has no length", v)
		case float64:
			return one(math.Abs(v))
		case string:
			return one(float64(utf8.RuneCountInString(v)))
		case []any:
			return one(float64(len(v)))
		case *jqObj:
			return one(float64(len(v.keys)))
		}
	case "utf8bytelength/0":
		if s, ok := in.(string); ok {
			return one(float64(len(s)))
		}
	case "keys/0", "keys_unsorted/0":
		switch v := in.(type) {
		case *jqObj:
			ks := append([]string(nil), v.keys...)
			if c.name == "keys" {
				sort.Strings(ks)
			}
			return one(toAnySlice(ks))
		case []any:
			out := make([]any, len(v))
			for i := range v {
				out[i] = float64(i)
			}
			return one(out)
		}
		return nil, jqErrf("%s has no keys", jqDescribe(in))
	case "values/0":
		if in == nil {
			return nil, nil
		}
		return one(in)
	case "has/1":
		k, err := arg(0)
		if err != nil {
			return nil, err
		}
		switch v := in.(type) {
		case *jqObj:
			ks, _ := k.(string)
			_, ok := v.m[ks]
			return one(ok)
		case []any:
			f, _ := k.(float64)
			return one(f >= 0 && int(f) < len(v))
		}
		return nil, jqErrf("Cannot check whether %s has a key", jqTypeName(in))
	case "in/1":
		obj, err := arg(0)
		if err != nil {
			return nil, err
		}
		return e.call(&jqCall{name: "has", args: []jqNode{&jqLiteral{v: in}}}, obj)
	case "type/0":
		return one(jqTypeName(in))
	case "map/1":
		return e.eval(&jqArray{body: &jqPipe{l: &jqIterate{target: jqIdentity{}}, r: c.args[0]}}, in)
	case "map_values/1":
		switch v := in.(type) {
		case *jqObj:
			o := newJqObj()
			for _, k := range v.keys {
				r, err := e.eval(c.args[0], v.m[k])
				if err != nil {
					return nil, err
				}
				if len(r) > 0 {
					o.set(k, r[0])
				}
			}
			return one(o)
		case []any:
			var out []any
			for _, x := range v {
				r, err := e.eval(c.args[0], x)
				if err != nil {
					return nil, err
				}
				if len(r) > 0 {
					out = append(out, r[0])
				}
			}
			return one(out)
		}
	case "select/1":
		conds, err := argVals(0)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, cv := range conds {
			if jqTruthy(cv) {
				out = append(out, in)
			}
		}
		return out, nil
	case "recurse/0":
		return e.eval(jqRecurse{}, in)
	case "recurse/1":
		var out []any
		var walk func(v any, depth int) error
		walk = func(v any, depth int) error {
			if depth > 10000 {
				return jqErrf("recursion too deep")
			}
			out = append(out, v)
			next, err := e.eval(&jqTry{x: c.args[0]}, v)
			if err != nil {
				return err
			}
			for _, n := range next {
				if err := walk(n, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		return out, walk(in, 0)
	case "to_entries/0":
		o, ok := in.(*jqObj)
		if !ok {
			return nil, jqErrf("%s has no keys", jqDescribe(in))
		}
		var out []any
		for _, k := range o.keys {
			ent := newJqObj()
			ent.set("key", k)
			ent.set("value", o.m[k])
			out = append(out, ent)
		}
		if out == nil {
			out = []any{}
		}
		return one(out)
	case "from_entries/0":
		arr, ok := in.([]any)
		if !ok {
			return nil, jqErrf("Cannot use %s as entries", jqDescribe(in))
		}
		o := newJqObj()
		for _, x := range arr {
			ent, ok := x.(*jqObj)
			if !ok {
				return nil, jqErrf("Cannot use %s as an entry", jqDescribe(x))
			}
			var key any
			for _, kn := range []string{"key", "k", "name", "Name", "Key", "K"} {
				if v, ok := ent.m[kn]; ok && v != nil {
					key = v
					break
				}
			}
			var val any
			for _, vn := range []string{"value", "v", "Value", "V"} {
				if v, ok := ent.m[vn]; ok {
					val = v
					break
				}
			}
			o.set(jqToString(key), val)
		}
		return one(o)
	case "with_entries/1":
		return e.eval(&jqPipe{l: &jqCall{name: "to_entries"}, r: &jqPipe{l: &jqCall{name: "map", args: c.args}, r: &jqCall{name: "from_entries"}}}, in)
	case "add/0":
		arr, err := jqValuesOf(in)
		if err != nil {
			return nil, err
		}
		var acc any
		for _, v := range arr {
			if acc, err = jqArith("+", acc, v); err != nil {
				return nil, err
			}
		}
		return one(acc)
	case "any/0", "all/0":
		arr, err := jqValuesOf(in)
		if err != nil {
			return nil, err
		}
		return one(jqQuantify(c.name, arr, jqTruthy))
	case "any/1", "all/1":
		arr, err := jqValuesOf(in)
		if err != nil {
			return nil, err
		}
		var evalErr error
		res := jqQuantify(c.name, arr, func(v any) bool {
			r, err := e.eval(c.args[0], v)
			if err != nil {
				evalErr = err
			}
			return len(r) > 0 && jqTruthy(r[0])
		})
		return []any{res}, evalErr
	case "flatten/0", "flatten/1":
		depth := 1e9
		if arity == 1 {
			d, err := arg(0)
			if err != nil {
				return nil, err
			}
			depth, _ = d.(float64)
		}
		arr, ok := in.([]any)
		if !ok {
			return nil, jqErrf("Cannot flatten %s", jqDescribe(in))
		}
		return one(jqFlatten(arr, depth))
	case "range/1", "range/2":
		var from, to float64
		if arity == 1 {
			v, err := arg(0)
			if err != nil {
				return nil, err
			}
			to, _ = v.(float64)
		} else {
			a, err := arg(0)
			if err != nil {
				return nil, err
			}
			b, err := arg(1)
			if err != nil {
				return nil, err
			}
			from, _ = a.(float64)
			to, _ = b.(float64)
		}
		var out []any
		for x := from; x < to && len(out) < maxLoopIterations; x++ {
			out = append(out, x)
		}
		return out, nil
	case "floor/0", "ceil/0", "round/0", "sqrt/0", "fabs/0", "abs/0":
		f, ok := in.(float64)
		if !ok {
			return nil, jqErrf("%s number required", jqDescribe(in))
		}
		fns := map[string]func(float64) float64{"floor": math.Floor, "ceil": math.Ceil, "round": math.Round, "sqrt": math.Sqrt, "fabs": math.Abs, "abs": math.Abs}
		return one(fns[c.name](f))
	case "tostring/0":
		return one(jqToString(in))
	case "tonumber/0":
		switch v := in.(type) {
		case float64:
			return one(v)
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, jqErrf("Cannot parse '%s' as JSON", v)
			}
			return one(f)
		}
		return nil, jqErrf("%s cannot be parsed as a number", jqDescribe(in))
	case "tojson/0":
		return one(encodeJSON(in, "", false))
	case "fromjson/0":
		s, ok := in.(string)
		if !ok {
			return nil, jqErrf("%s cannot be parsed as JSON", jqDescribe(in))
		}
		vals, err := decodeJSONStream(s)
		if err != nil || len(vals) == 0 {
			return nil, jqErrf("%s (while parsing '%s')", err, s)
		}
		return one(vals[0])
	case "ascii_downcase/0", "ascii_upcase/0", "ltrimstr/1", "rtrimstr/1", "startswith/1", "endswith/1", "split/1", "test/1", "test/2",
		"sub/2", "gsub/2", "sub/3", "gsub/3", "explode/0", "trim/0", "ltrim/0", "rtrim/0", "index/1", "rindex/1", "indices/1", "contains/1", "inside/1", "join/1", "implode/0":
		return e.stringFn(c, in, arg)
	case "sort/0", "unique/0", "reverse/0", "min/0", "max/0", "first/0", "last/0":
		return jqArrayFn(c.name, in)
	case "sort_by/1", "group_by/1", "unique_by/1", "min_by/1", "max_by/1":
		return e.byFn(c, in)
	case "first/1", "last/1", "limit/2":
		return e.takeFn(c, in, arg)
	case "walk/1":
		v, err := e.walk(c.args[0], in)
		if err != nil {
			return nil, err
		}
		return one(v)
	case "paths/0":
		var out []any
		jqPaths(in, nil, func(p []any, _ any) { out = append(out, append([]any{}, p...)) })
		return out, nil
	case "leaf_paths/0":
		var out []any
		jqPaths(in, nil, func(p []any, v any) {
			switch v.(type) {
			case []any, *jqObj:
			default:
				out = append(out, append([]any{}, p...))
			}
		})
		return out, nil
	case "getpath/1":
		p, err := arg(0)
		if err != nil {
			return nil, err
		}
		cur := in
		path, _ := p.([]any)
		for _, k := range path {
			if cur, err = jqIndexValue(cur, k); err != nil {
				return nil, err
			}
		}
		return one(cur)
	case "arrays/0", "objects/0", "iterables/0", "booleans/0", "numbers/0", "strings/0", "nulls/0", "scalars/0":
		t := jqTypeName(in)
		keep := map[string]bool{
			"arrays": t == "array", "objects": t == "object", "iterables": t == "array" || t == "object",
			"booleans": t == "boolean", "numbers": t == "number", "strings": t == "string", "nulls": t == "null",
			"scalars": t != "array" && t != "object",
		}[c.name]
		if keep {
			return one(in)
		}
		return nil, nil
	case "env/0":
		return one(jqEnvObject())
	case "input_filename/0", "halt/0", "now/0":
		if c.name == "now/0" {
			return nil, nil
		}
		return one(nil)
	case "del/1", "to_number/0", "input/0", "inputs/0", "path/1", "setpath/2", "delpaths/1", "splits/1", "capture/1", "match/1", "scan/1", "ascii/0":
		return nil, jqError{c.name + " is not supported by the in-browser shell"}
	}
	if strings.HasPrefix(c.name, "@") {
		s, err := jqFormatValue(c.name[1:], in)
		return []any{s}, err
	}
	return nil, jqError{fmt.Sprintf("%s/%d is not defined or not supported by the in-browser shell", c.name, arity)}
}
